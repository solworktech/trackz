mod datepicker;
mod db;
mod icon_resolver;
mod sleep_listener;
mod ui;
mod utils;
use chrono::{DateTime, Duration, Local, Timelike, Utc};
use eframe::egui;
use egui::ScrollArea;
use parking_lot::RwLock;
use std::sync::Arc;

#[tokio::main]
async fn main() -> Result<(), eframe::Error> {
    // Start sleep listener
    tokio::spawn(async {
        if let Err(e) = sleep_listener::SleepListener::listen().await {
            eprintln!("Sleep listener error: {}", e);
        }
    });

    let options = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default().with_inner_size([1600.0, 900.0]),
        ..Default::default()
    };

    eframe::run_native(
        "Trackz Visualization",
        options,
        Box::new(|_cc| Ok(Box::new(TrackzApp::new()))),
    )
}

pub struct TrackzApp {
    selected_tab: TabView,
    app_state: Arc<RwLock<AppState>>,
    time_filter: TimeFilter,
    custom_query: String,
    custom_chart_type: ChartType,
    show_custom_timeframe: bool,
    custom_row_limit: String,
    custom_start_picker: chrono::NaiveDate,
    custom_end_picker: chrono::NaiveDate,
    custom_filter_error: String,
    // table_sorted_column: Option<String>,
    // table_sort_ascending: bool,
}

#[derive(Clone, Copy, PartialEq)]
enum TabView {
    Dashboard,
    TopApps,
    TimeSpent,
    WindowActivity,
    DailyStats,
    CustomQuery,
}

#[derive(Clone)]
pub struct TimeFilter {
    pub start_time: DateTime<Utc>,
    pub end_time: DateTime<Utc>,
}

impl Default for TimeFilter {
    fn default() -> Self {
        let end = Utc::now();
        let start = end
            .clone()
            .with_hour(0)
            .unwrap()
            .with_minute(0)
            .unwrap()
            .with_second(0)
            .unwrap();
        Self {
            start_time: start,
            end_time: end,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum ChartType {
    BarChart,
    LineChart,
    PieChart,
    TimeSeriesChart,
    Table,
}

struct AppState {
    db: Option<db::Database>,
    last_query_result: Option<Vec<db::QueryResult>>,
    last_column_names: Vec<String>,
    error: Option<String>,
}

impl TrackzApp {
    fn new() -> Self {
        let db_path_buf = utils::get_db_path();
        let db_path = db_path_buf.to_string_lossy().to_string();

        // Create today's time filter
        let end = Utc::now();
        let start = end
            .clone()
            .with_hour(0)
            .unwrap()
            .with_minute(0)
            .unwrap()
            .with_second(0)
            .unwrap();
        let today_filter = TimeFilter {
            start_time: start,
            end_time: end,
        };

        let app_state = Arc::new(RwLock::new(AppState {
            db: db::Database::new(&db_path).ok(),
            last_column_names: Vec::new(),
            last_query_result: None,
            error: None,
        }));

        let mut app = Self {
            selected_tab: TabView::Dashboard,
            app_state,
            time_filter: today_filter,
            custom_query: String::new(),
            custom_chart_type: ChartType::BarChart,
            show_custom_timeframe: false,
            custom_row_limit: "20".to_string(),
            custom_start_picker: Local::now().naive_local().date(),
            custom_end_picker: Local::now().naive_local().date(),
            custom_filter_error: String::new(),
            // table_sorted_column: None,
            // table_sort_ascending: false,
        };

        // Load today's data automatically
        app.load_dashboard_data();
        app
    }

    fn load_dashboard_data(&mut self) {
        let db = self.app_state.read().db.as_ref().map(|d| d.clone());
        if let Some(database) = db {
            match database.get_top_apps_pie_chart(
                self.custom_row_limit.parse::<i32>().unwrap(),
                self.time_filter.clone(),
            ) {
                Ok((column_names, results)) => {
                    self.app_state.write().last_query_result = Some(results);
                    self.app_state.write().last_column_names = column_names;
                    self.app_state.write().error = None;
                }
                Err(e) => {
                    self.app_state.write().error = Some(e.to_string());
                }
            }
        }
    }
}

impl eframe::App for TrackzApp {
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        egui::CentralPanel::default().show(ctx, |ui| {
            // Bigger header
            ui.heading(
                egui::RichText::new("📊 Trackz Visualization Dashboard")
                    .size(24.0)
                    .strong(),
            );

            // Bigger gap
            ui.add_space(15.0);

            // Time filter controls
            ui.horizontal(|ui| {
                ui.label(egui::RichText::new("Time Range:").size(14.0));

                if ui.button("Today").clicked() {
                    let end = Utc::now();
                    let start = end
                        .clone()
                        .with_hour(0)
                        .unwrap()
                        .with_minute(0)
                        .unwrap()
                        .with_second(0)
                        .unwrap();
                    self.time_filter = TimeFilter {
                        start_time: start,
                        end_time: end,
                    };
                    self.load_dashboard_data();
                }

                if ui.button("Last 7 days").clicked() {
                    self.time_filter = TimeFilter {
                        start_time: Utc::now() - Duration::days(7),
                        end_time: Utc::now(),
                    };
                    self.load_dashboard_data();
                }
                if ui.button("Last 30 days").clicked() {
                    self.time_filter = TimeFilter {
                        start_time: Utc::now() - Duration::days(30),
                        end_time: Utc::now(),
                    };
                    self.load_dashboard_data();
                }
                if ui.button("Last 90 days").clicked() {
                    self.time_filter = TimeFilter {
                        start_time: Utc::now() - Duration::days(90),
                        end_time: Utc::now(),
                    };
                    self.load_dashboard_data();
                }

                if ui.button("Custom filters").clicked() {
                    self.show_custom_timeframe = true;
                }
            });

            // Custom filters dialog
            ui.separator();

            if self.show_custom_timeframe {
                if egui::Window::new("Custom Filters")
                    .show(ctx, |ui| {
                        ui.label("Start Date:");
                        if let Some(new_date) = crate::datepicker::DatePicker::new(
                            "start_picker",
                            self.custom_start_picker,
                        )
                        .show(ui)
                        {
                            self.custom_start_picker = new_date;
                        }

                        ui.label("End Date:");
                        if let Some(new_date) =
                            crate::datepicker::DatePicker::new("end_picker", self.custom_end_picker)
                                .show(ui)
                        {
                            self.custom_end_picker = new_date;
                        }

                        ui.label("Number of rows:");
                        ui.text_edit_singleline(&mut self.custom_row_limit);

                        if !self.custom_filter_error.is_empty() {
                            ui.colored_label(egui::Color32::RED, &self.custom_filter_error);
                        }

                        ui.horizontal(|ui| {
                            if ui.button("Apply").clicked() {
                                self.time_filter = TimeFilter {
                                    start_time: self
                                        .custom_start_picker
                                        .and_hms_opt(0, 0, 0)
                                        .unwrap()
                                        .and_utc(),
                                    end_time: self
                                        .custom_end_picker
                                        .and_hms_opt(23, 59, 59)
                                        .unwrap()
                                        .and_utc(),
                                };
                                self.custom_filter_error.clear();
                                self.load_dashboard_data();
                                self.show_custom_timeframe = false;
                            }
                            if ui.button("Cancel").clicked() {
                                self.show_custom_timeframe = false;
                            }
                        });
                    })
                    .is_none()
                {
                    self.show_custom_timeframe = false;
                }
            }

            // Tab selection
            ui.horizontal(|ui| {
                ui.selectable_value(&mut self.selected_tab, TabView::Dashboard, "Dashboard");
                ui.selectable_value(&mut self.selected_tab, TabView::TopApps, "Top Apps");
                ui.selectable_value(&mut self.selected_tab, TabView::TimeSpent, "Time Spent");
                ui.selectable_value(
                    &mut self.selected_tab,
                    TabView::WindowActivity,
                    "Window Activity",
                );
                ui.selectable_value(&mut self.selected_tab, TabView::DailyStats, "Daily Stats");
                ui.selectable_value(&mut self.selected_tab, TabView::CustomQuery, "Custom Query");
            });

            ui.separator();

            // Content based on selected tab
            match self.selected_tab {
                TabView::Dashboard => self.render_dashboard(ui),
                TabView::TopApps => self.render_top_apps(ui),
                TabView::TimeSpent => self.render_time_spent(ui),
                TabView::WindowActivity => self.render_window_activity(ui),
                TabView::DailyStats => self.render_daily_stats(ui),
                TabView::CustomQuery => self.render_custom_query(ui),
            }

            // Display any errors
            if let Some(error) = &self.app_state.read().error {
                ui.colored_label(egui::Color32::RED, format!("❌ Error: {}", error));
            }
        });
    }
}

impl TrackzApp {
    fn render_dashboard(&mut self, ui: &mut egui::Ui) {
        ui.horizontal(|ui| {
            ui.heading(
                egui::RichText::new(&format!(
                    "{} Top Used Activities for {}",
                    self.custom_row_limit,
                    self.get_timeframe_label()
                ))
                .size(18.0),
            );

            if ui.button("Export as CSV").clicked() {
                self.export_dashboard_csv();
            }
        });

        if let Some(results) = &self.app_state.read().last_query_result {
            // Pie chart in a scrollable area
            ScrollArea::vertical().show(ui, |ui| {
                ui::draw_interactive_pie_chart(ui, results);
                ui.separator();

                ui.label(egui::RichText::new("Data Table").size(14.0).strong());
                ui.separator();
                ui::draw_results_table_content(
                    ui,
                    results,
                    &self.app_state.read().last_column_names,
                );
            });
        } else {
            ui.label("No data available. Click a time range button to load data.");
        }
    }

    fn get_timeframe_label(&self) -> String {
        let start = self.time_filter.start_time;
        let end = self.time_filter.end_time;

        let now = Utc::now();
        let today_start = now
            .with_hour(0)
            .unwrap()
            .with_minute(0)
            .unwrap()
            .with_second(0)
            .unwrap();

        if (start.timestamp() - today_start.timestamp()).abs() < 60
            && (end.timestamp() - now.timestamp()).abs() < 60
        {
            "Today".to_string()
        } else {
            format!("{} to {}", start.format("%Y-%m-%d"), end.format("%Y-%m-%d"))
        }
    }

    fn export_dashboard_csv(&self) {
        if let Some(results) = &self.app_state.read().last_query_result {
            // Show file dialog
            if let Some(path) = rfd::FileDialog::new()
                .set_file_name("trackz-data.csv")
                .save_file()
            {
                /*let mut csv = "Process,Window,Duration (Hours)\n".to_string();
                for result in results {
                    csv.push_str(&format!("\"{}\",{}\n", result.label, result.value));
                }

                // Write to file
                match std::fs::write(&path, &csv) {
                    Ok(_) => {
                        println!("✅ CSV exported successfully to: {}", path.display());
                    }
                    Err(e) => {
                        eprintln!("❌ Failed to write CSV file: {}", e);
                    }
                }*/
            }
        }
    }

    fn render_top_apps(&mut self, ui: &mut egui::Ui) {
        ui.label("Top 10 Apps by Total Focus Time");

        if ui.button("Refresh").clicked() {
            let db = self.app_state.read().db.as_ref().map(|d| d.clone());
            if let Some(database) = db {
                match database.get_top_apps_by_time(10, self.time_filter.clone()) {
                    Ok(results) => {
                        self.app_state.write().last_query_result = Some(results);
                        self.app_state.write().error = None;
                    }
                    Err(e) => {
                        self.app_state.write().error = Some(e.to_string());
                    }
                }
            }
        }

        if let Some(results) = &self.app_state.read().last_query_result {
            ui::draw_bar_chart(ui, results);
        }
    }

    fn render_time_spent(&mut self, ui: &mut egui::Ui) {
        ui.label("Total Time Spent per App");

        if ui.button("Refresh").clicked() {
            let db = self.app_state.read().db.as_ref().map(|d| d.clone());
            if let Some(database) = db {
                match database.get_time_spent_per_app(self.time_filter.clone()) {
                    Ok(results) => {
                        self.app_state.write().last_query_result = Some(results);
                        self.app_state.write().error = None;
                    }
                    Err(e) => {
                        self.app_state.write().error = Some(e.to_string());
                    }
                }
            }
        }

        if let Some(results) = &self.app_state.read().last_query_result {
            ui::draw_interactive_pie_chart(ui, results);
        }
    }

    fn render_window_activity(&mut self, ui: &mut egui::Ui) {
        ui.label("Window Activity Over Time");

        if ui.button("Refresh").clicked() {
            let db = self.app_state.read().db.as_ref().map(|d| d.clone());
            if let Some(database) = db {
                match database.get_window_activity_timeline(self.time_filter.clone()) {
                    Ok(results) => {
                        self.app_state.write().last_query_result = Some(results);
                        self.app_state.write().error = None;
                    }
                    Err(e) => {
                        self.app_state.write().error = Some(e.to_string());
                    }
                }
            }
        }

        if let Some(results) = &self.app_state.read().last_query_result {
            ui::draw_line_chart(ui, results);
        }
    }

    fn render_daily_stats(&mut self, ui: &mut egui::Ui) {
        ui.label("Daily Usage Statistics");

        if ui.button("Refresh").clicked() {
            let db = self.app_state.read().db.as_ref().map(|d| d.clone());
            if let Some(database) = db {
                match database.get_daily_stats(self.time_filter.clone()) {
                    Ok(results) => {
                        self.app_state.write().last_query_result = Some(results);
                        self.app_state.write().error = None;
                    }
                    Err(e) => {
                        self.app_state.write().error = Some(e.to_string());
                    }
                }
            }
        }

        if let Some(results) = &self.app_state.read().last_query_result {
            ui::draw_time_series_chart(ui, results);
        }
    }

    fn render_custom_query(&mut self, ui: &mut egui::Ui) {
        ui.label("Execute Custom SQL Query");

        ui.horizontal(|ui| {
            ui.label("Chart Type:");
            ui.selectable_value(&mut self.custom_chart_type, ChartType::BarChart, "Bar");
            ui.selectable_value(&mut self.custom_chart_type, ChartType::LineChart, "Line");
            ui.selectable_value(&mut self.custom_chart_type, ChartType::PieChart, "Pie");
            ui.selectable_value(
                &mut self.custom_chart_type,
                ChartType::TimeSeriesChart,
                "Time Series",
            );
            ui.selectable_value(&mut self.custom_chart_type, ChartType::Table, "Table");
        });

        ui.label("SQL Query:");
        ui.text_edit_multiline(&mut self.custom_query);

        ui.add_space(5.0);
        if ui.button("Execute & Visualize").clicked() && !self.custom_query.is_empty() {
            let db = self.app_state.read().db.as_ref().map(|d| d.clone());
            if let Some(database) = db {
                match database.execute_query(&self.custom_query) {
                    Ok((column_names, results)) => {
                        self.app_state.write().last_query_result = Some(results);
                        self.app_state.write().last_column_names = column_names;
                        self.app_state.write().error = None;
                    }
                    Err(e) => {
                        self.app_state.write().error = Some(e.to_string());
                    }
                }
            }
        }
        ui.separator();

        if let Some(results) = &self.app_state.read().last_query_result {
            match self.custom_chart_type {
                ChartType::BarChart => ui::draw_bar_chart(ui, results),
                ChartType::LineChart => ui::draw_line_chart(ui, results),
                ChartType::PieChart => ui::draw_interactive_pie_chart(ui, results),
                ChartType::TimeSeriesChart => ui::draw_time_series_chart(ui, results),
                ChartType::Table => {
                    ui::draw_results_table(ui, results, &self.app_state.read().last_column_names)
                }
            }
        }
    }
}
