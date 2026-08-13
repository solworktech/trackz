use chrono::{Datelike, Duration, Local, Month, NaiveDate, Weekday};
use egui::{Color32, DragValue, Frame, Id, Key, Order, RichText, Ui};
use num_traits::cast::FromPrimitive;

/// Date picker widget for selecting dates
pub struct DatePicker {
    id: Id,
    date: NaiveDate,
    sunday_first: bool,
    format_string: String,
    weekend_color: Color32,
    highlight_weekend: bool,
}

impl DatePicker {
    /// Create a new date picker with unique id and date
    pub fn new<T: std::hash::Hash + ?Sized>(id: &T, date: NaiveDate) -> Self {
        Self {
            id: Id::new(id),
            date,
            sunday_first: false,
            format_string: String::from("%Y-%m-%d"),
            weekend_color: Color32::from_rgb(196, 0, 0),
            highlight_weekend: true,
        }
    }

    /// Draw names of week days as 7 columns of grid
    fn show_grid_header(&self, ui: &mut Ui) {
        let day_indexes = if self.sunday_first {
            [6, 0, 1, 2, 3, 4, 5]
        } else {
            [0, 1, 2, 3, 4, 5, 6]
        };
        for i in day_indexes {
            let b = Weekday::from_u32(i).unwrap();
            ui.label(b.to_string());
        }
    }

    /// Get number of days offset for calendar start
    fn get_start_offset_of_calendar(&self, first_day: NaiveDate) -> u32 {
        if self.sunday_first {
            first_day.weekday().num_days_from_sunday()
        } else {
            first_day.weekday().num_days_from_monday()
        }
    }

    /// Get number of days offset for calendar end
    fn get_end_offset_of_calendar(&self, first_day: NaiveDate) -> u32 {
        if self.sunday_first {
            (7 - first_day.weekday().num_days_from_sunday()) % 7
        } else {
            (7 - first_day.weekday().num_days_from_monday()) % 7
        }
    }

    /// Show the calendar grid
    fn show_calendar_grid(&mut self, ui: &mut Ui) -> Option<NaiveDate> {
        let mut selected_date = None;

        egui::Grid::new(self.id.with("calendar")).show(ui, |ui| {
            self.show_grid_header(ui);

            let first_day_of_current_month =
                NaiveDate::from_ymd_opt(self.date.year(), self.date.month(), 1).unwrap();
            let start_offset = self.get_start_offset_of_calendar(first_day_of_current_month);
            let days_in_month = get_days_from_month(self.date.year(), self.date.month());
            let first_day_of_next_month =
                first_day_of_current_month + Duration::days(days_in_month);
            let end_offset = self.get_end_offset_of_calendar(first_day_of_next_month);
            let start_date = first_day_of_current_month - Duration::days(start_offset.into());

            for i in 0..(start_offset as i64 + days_in_month + end_offset as i64) {
                if i % 7 == 0 {
                    ui.end_row();
                }
                let d = start_date + Duration::days(i);
                if self.show_day_button(d, ui) {
                    selected_date = Some(d);
                }
            }
        });

        selected_date
    }

    /// Show individual day button
    fn show_day_button(&mut self, date: NaiveDate, ui: &mut Ui) -> bool {
        let is_current_day = self.date == date;
        let is_current_month = self.date.month() == date.month();

        let mut clicked = false;

        ui.add_enabled_ui(!is_current_day, |ui| {
            ui.centered_and_justified(|ui| {
                if !is_current_month {
                    ui.style_mut().visuals.button_frame = false;
                }

                if self.highlight_weekend && self.is_weekend(date) && is_current_month {
                    ui.style_mut().visuals.override_text_color = Some(self.weekend_color);
                }

                let mut button_text = RichText::new(date.day().to_string());
                if is_current_day {
                    button_text = button_text.background_color(Color32::BLUE);
                }

                clicked = ui.button(button_text).clicked();
            });
        });

        clicked
    }

    /// Check if date is a weekend
    fn is_weekend(&self, date: NaiveDate) -> bool {
        date.weekday() == Weekday::Sat || date.weekday() == Weekday::Sun
    }

    /// Show month and year controls
    fn show_header(&mut self, ui: &mut Ui) {
        ui.horizontal(|ui| {
            self.show_month_control(ui);
            ui.separator();
            self.show_year_control(ui);
            if ui.button("Today").clicked() {
                self.date = Local::now().naive_local().date();
            }
        });
    }

    /// Show month control with buttons
    fn show_month_control(&mut self, ui: &mut Ui) {
        if ui.button("◀").clicked() {
            self.date = self.date - Duration::days(30);
        }

        let month_string = Month::from_u32(self.date.month()).unwrap().name();
        ui.label(RichText::new(format!("{: <9}", month_string)).monospace());

        if ui.button("▶").clicked() {
            self.date = self.date + Duration::days(30);
        }
    }

    /// Show year control with drag value
    fn show_year_control(&mut self, ui: &mut Ui) {
        if ui.button("◀").clicked() {
            self.date = self.date - Duration::days(365);
        }

        let mut drag_year = self.date.year();
        ui.add(DragValue::new(&mut drag_year).range(1900..=2100));
        if drag_year != self.date.year() {
            self.date = NaiveDate::from_ymd_opt(drag_year, self.date.month(), self.date.day())
                .unwrap_or(self.date);
        }

        if ui.button("▶").clicked() {
            self.date = self.date + Duration::days(365);
        }
    }

    /// Show the date picker UI and return selected date if changed
    pub fn show(mut self, ui: &mut Ui) -> Option<NaiveDate> {
        let formatted_date = self.date.format(&self.format_string);
        let button_response = ui.button(formatted_date.to_string());

        let mut selected = None;

        if button_response.clicked() {
            ui.memory_mut(|mem| mem.toggle_popup(self.id));
        }

        if ui.memory(|mem| mem.is_popup_open(self.id)) {
            // Retrieve persisted date state
            ui.data_mut(|data| {
                if let Some(date) = data.get_persisted::<NaiveDate>(self.id.with("date")) {
                    self.date = date;
                }
            });

            let area_id = self.id.with("popup_area");
            egui::Area::new(area_id)
                .order(Order::Foreground)
                .default_pos(button_response.rect.left_bottom())
                .movable(false)
                .show(ui.ctx(), |ui| {
                    Frame::popup(ui.style()).show(ui, |ui| {
                        self.show_header(ui);
                        if let Some(date) = self.show_calendar_grid(ui) {
                            selected = Some(date);
                            ui.memory_mut(|mem| mem.toggle_popup(self.id));
                        }
                    });
                });

            // Persist updated date state
            ui.data_mut(|data| {
                data.insert_persisted(self.id.with("date"), self.date);
            });

            // Close on escape
            if ui.input(|input| input.key_pressed(Key::Escape)) {
                ui.memory_mut(|mem| mem.toggle_popup(self.id));
            }
        }

        selected
    }
}

/// Get number of days in a month
fn get_days_from_month(year: i32, month: u32) -> i64 {
    let first_of_month = NaiveDate::from_ymd_opt(year, month, 1).unwrap();
    let first_of_next_month = if month == 12 {
        NaiveDate::from_ymd_opt(year + 1, 1, 1).unwrap()
    } else {
        NaiveDate::from_ymd_opt(year, month + 1, 1).unwrap()
    };

    (first_of_next_month - first_of_month).num_days()
}
