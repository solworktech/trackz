use crate::db::QueryResult;
use crate::icon_resolver::IconResolver;
use egui::{Color32, Pos2, ScrollArea, Ui, Vec2};

use crate::utils::{draw_tooltip, format_duration_hms, get_color_palette, truncate_label};

// for values (app_name + window_name) of pie chart and table
const MAX_STR_CHARS: usize = 100;

/// Draw a filled pie slice
fn draw_pie_slice_filled(
    painter: &egui::Painter,
    center: Pos2,
    radius: f32,
    start_angle: f32,
    arc_angle: f32,
    color: Color32,
) {
    let segments = ((arc_angle.abs() * 60.0) as usize).max(3).min(200);
    let mut points = vec![center];

    for i in 0..=segments {
        let angle = start_angle + (i as f32 / segments as f32) * arc_angle;
        let x = center.x + radius * angle.cos();
        let y = center.y + radius * angle.sin();
        points.push(Pos2::new(x, y));
    }

    painter.add(egui::Shape::convex_polygon(
        points,
        color,
        egui::Stroke::NONE,
    ));
}

fn draw_legend_items_with_icons(ui: &mut Ui, results: &[QueryResult]) {
    let colors = get_color_palette();

    for (idx, result) in results.iter().enumerate() {
        let color = colors[idx % colors.len()];
        let label = result.get_string("label").unwrap_or_default();
        let duration = result.get_float("duration").unwrap_or(0.0);
        let process_name = result.get_string("process_name").unwrap_or_default();

        ui.horizontal(|ui| {
            // Color box
            let (_response, painter) =
                ui.allocate_painter(Vec2::new(15.0, 15.0), egui::Sense::hover());
            painter.rect_filled(
                egui::Rect::from_min_size(painter.clip_rect().min, Vec2::new(15.0, 15.0)),
                0.0,
                color,
            );

            // Icon (if available)
            if let Some(icon_path) =
                IconResolver::get_icon_path(&process_name, &["16x16", "32x32", "128x128"])
            {
                if let Ok(img) = image::open(&icon_path) {
                    // Resize to 16x16
                    let resized = img.resize_exact(16, 16, image::imageops::FilterType::Lanczos3);
                    let rgba_img = resized.to_rgba8();
                    let size = [rgba_img.width() as usize, rgba_img.height() as usize];
                    let pixels = rgba_img.to_vec();

                    let color_image = egui::ColorImage::from_rgba_unmultiplied(size, &pixels);

                    let texture_handle = ui.ctx().load_texture(
                        &icon_path,
                        color_image,
                        egui::TextureOptions::LINEAR,
                    );

                    ui.image(&texture_handle);
                }
            }

            let truncated_label = truncate_label(&label, MAX_STR_CHARS);
            let time_str = format_duration_hms(duration);

            ui.label(format!("{} - {}", truncated_label, time_str));
        });
    }
}

/// Draw an interactive pie chart with hover tooltips and responsive layout
pub fn draw_interactive_pie_chart(ui: &mut Ui, results: &[QueryResult]) {
    if results.is_empty() {
        ui.label("No data available");
        return;
    }

    let total: f64 = results.iter().filter_map(|r| r.get_float("duration")).sum();

    if total == 0.0 {
        ui.label("No data available");
        return;
    }

    let available_width = ui.available_width();
    let legend_height = (results.len() as f32 * 20.0) + 40.0;
    let chart_height = 500.0_f32.min(legend_height * 2.5);

    let use_side_by_side = available_width > 1400.0;

    if use_side_by_side {
        ui.horizontal(|ui| {
            ui.vertical(|ui| {
                draw_pie_chart_interior(ui, results, total, available_width * 0.55, chart_height);
            });

            ui.separator();

            /*ui.vertical(|ui| {
                //ui.label(egui::RichText::new("Legend").size(14.0).strong());
                draw_legend_items_with_icons(ui, results);
            });*/
            ui.vertical(|ui| {
                let available_height = ui.available_height();
                let legend_height = (results.len() as f32 * 20.0) + 40.0;
                let spacing = (available_height - legend_height) / 2.0;

                ui.add_space(spacing);
                draw_legend_items_with_icons(ui, results);
            });
        });
    } else {
        ui.vertical(|ui| {
            draw_pie_chart_interior(ui, results, total, available_width * 0.9, chart_height);

            ui.separator();
            ui.label(egui::RichText::new("Legend").size(14.0).strong());
            ui.separator();
            draw_legend_items_with_icons(ui, results);
        });
    }
}

/// Internal pie chart drawing with hover detection
/*fn draw_pie_chart_interior(
    ui: &mut Ui,
    results: &[QueryResult],
    total: f64,
    width: f32,
    height: f32,
) {
    let rect = ui.allocate_rect(
        egui::Rect::from_min_size(ui.cursor().min, Vec2::new(width, height)),
        egui::Sense::hover(),
    );
    let painter = ui.painter();

    let center = Pos2::new(rect.rect.center().x, rect.rect.min.y + height / 2.0);
    let radius = (height / 2.0) * 0.7;

    let colors = get_color_palette();
    let mouse_pos = ui.input(|i| i.pointer.hover_pos());
    let mut angle = 0.0;
    let mut hovered_slice: Option<usize> = None;

    // Calculate slices and detect hover
    let mut slices = Vec::new();
    for (idx, result) in results.iter().enumerate() {
        let value = result.get_float("duration").unwrap_or(0.0);
        let percentage = value / total;
        let arc_angle = (percentage * 2.0 * std::f64::consts::PI) as f32;
        slices.push((angle, arc_angle));

        if let Some(pos) = mouse_pos {
            let dx = pos.x - center.x;
            let dy = pos.y - center.y;
            let dist = (dx * dx + dy * dy).sqrt();

            if dist < radius && dist > radius * 0.2 {
                let mouse_angle = dy.atan2(dx);
                let normalized_angle = if mouse_angle < 0.0 {
                    mouse_angle + 2.0 * std::f32::consts::PI
                } else {
                    mouse_angle
                };

                let normalized_slice_start = if angle < 0.0 {
                    angle + 2.0 * std::f32::consts::PI
                } else {
                    angle
                };

                if normalized_angle >= normalized_slice_start
                    && normalized_angle <= normalized_slice_start + arc_angle
                {
                    hovered_slice = Some(idx);
                }
            }
        }

        angle += arc_angle;
    }

    // Draw slices
    for (idx, (slice_angle, arc_angle)) in slices.iter().enumerate() {
        let color = colors[idx % colors.len()];
        let is_hovered = hovered_slice == Some(idx);
        let draw_color = if is_hovered {
            Color32::from_rgb(
                (color.r() as f32 * 1.3) as u8,
                (color.g() as f32 * 1.3) as u8,
                (color.b() as f32 * 1.3) as u8,
            )
        } else {
            color
        };

        draw_pie_slice_filled(
            painter,
            center,
            radius,
            *slice_angle,
            *arc_angle,
            draw_color,
        );

        // Draw percentage label
        let label_angle = slice_angle + arc_angle / 2.0;
        let label_dist = radius * 0.65;
        let label_pos = Pos2::new(
            center.x + label_dist * label_angle.cos(),
            center.y + label_dist * label_angle.sin(),
        );
        let percentage_label =
            (results[idx].get_float("duration").unwrap_or(0.0) / total * 100.0) as i32;
        if percentage_label > 1 {
            painter.text(
                label_pos,
                egui::Align2::CENTER_CENTER,
                &format!(
                    "{}%",
                    (results[idx].get_float("duration").unwrap_or(0.0) / total * 100.0) as i32
                ),
                egui::FontId::proportional(14.0),
                Color32::WHITE,
            );
        }
    }

    // Draw hover tooltip
    if let Some(idx) = hovered_slice {
        if let Some(mouse_pos) = mouse_pos {
            let result = &results[idx];
            let label = result.get_string("label").unwrap_or_default();
            let value = result.get_float("duration").unwrap_or(0.0);
            let tooltip_text = format!(
                "{}\n{} ({:.1}%)",
                label,
                format_duration_hms(value),
                value / total * 100.0
            );
            draw_tooltip(&painter, &tooltip_text, mouse_pos, center.x);
        }
    }
}*/

fn draw_pie_chart_interior(
    ui: &mut Ui,
    results: &[QueryResult],
    total: f64,
    width: f32,
    height: f32,
) {
    let rect = ui.allocate_rect(
        egui::Rect::from_min_size(ui.cursor().min, Vec2::new(width, height)),
        egui::Sense::hover(),
    );
    let painter = ui.painter();
    let center = Pos2::new(rect.rect.center().x, rect.rect.min.y + height / 2.0);
    let radius = (height / 2.0) * 0.7;

    let colors = get_color_palette();
    let mouse_pos = ui.input(|i| i.pointer.hover_pos());
    let mut angle = 0.0;
    let mut hovered_slice: Option<usize> = None;

    let mut slices = Vec::new();
    for (idx, result) in results.iter().enumerate() {
        let value = result.get_float("duration").unwrap_or(0.0);
        let percentage = value / total;
        let arc_angle = (percentage * 2.0 * std::f64::consts::PI) as f32;
        slices.push((angle, arc_angle, percentage));

        if let Some(pos) = mouse_pos {
            let dx = pos.x - center.x;
            let dy = pos.y - center.y;
            let dist = (dx * dx + dy * dy).sqrt();

            if dist < radius && dist > radius * 0.2 {
                let mouse_angle = dy.atan2(dx);
                let normalized_angle = if mouse_angle < 0.0 {
                    mouse_angle + 2.0 * std::f32::consts::PI
                } else {
                    mouse_angle
                };

                let normalized_slice_start = if angle < 0.0 {
                    angle + 2.0 * std::f32::consts::PI
                } else {
                    angle
                };

                if normalized_angle >= normalized_slice_start
                    && normalized_angle <= normalized_slice_start + arc_angle
                {
                    hovered_slice = Some(idx);
                }
            }
        }

        angle += arc_angle;
    }

    // Draw slices
    for (idx, (slice_angle, arc_angle, percentage)) in slices.iter().enumerate() {
        let color = colors[idx % colors.len()];
        let is_hovered = hovered_slice == Some(idx);
        let draw_color = if is_hovered {
            Color32::from_rgb(
                (color.r() as f32 * 1.3) as u8,
                (color.g() as f32 * 1.3) as u8,
                (color.b() as f32 * 1.3) as u8,
            )
        } else {
            color
        };

        draw_pie_slice_filled(
            &painter,
            center,
            radius,
            *slice_angle,
            *arc_angle,
            draw_color,
        );

        let label_angle = slice_angle + arc_angle / 2.0;
        let label_dist = radius * 0.65;
        let label_pos = Pos2::new(
            center.x + label_dist * label_angle.cos(),
            center.y + label_dist * label_angle.sin(),
        );

        let percentage_label = (*percentage * 100.0) as i32;
        if percentage_label > 1 {
            // Draw percentage label
            painter.text(
                label_pos,
                egui::Align2::CENTER_CENTER,
                &format!("{}%", percentage_label),
                egui::FontId::proportional(14.0),
                Color32::WHITE,
            );
        }

        // Draw icon for slices > 5%
        if percentage_label > 5 {
            let result = &results[idx];
            if let Some(process_name) = result.get_string("process_name") {
                if let Some(icon_path) =
                    IconResolver::get_icon_path(&process_name, &["16x16", "32x32", "128x128"])
                {
                    if let Ok(img) = image::open(&icon_path) {
                        let resized =
                            img.resize_exact(20, 20, image::imageops::FilterType::Lanczos3);
                        let rgba_img = resized.to_rgba8();
                        let size = [rgba_img.width() as usize, rgba_img.height() as usize];
                        let pixels = rgba_img.to_vec();

                        let color_image = egui::ColorImage::from_rgba_unmultiplied(size, &pixels);

                        let texture_handle = ui.ctx().load_texture(
                            &format!("pie_icon_{}", idx),
                            color_image,
                            egui::TextureOptions::LINEAR,
                        );

                        let icon_pos = Pos2::new(
                            center.x + (radius * 0.4) * label_angle.cos(),
                            center.y + (radius * 0.4) * label_angle.sin(),
                        );

                        let icon_radius = 15.0;

                        // Draw black circle background
                        painter.circle_filled(icon_pos, icon_radius, Color32::BLACK);

                        // Draw icon on top
                        painter.image(
                            texture_handle.id(),
                            egui::Rect::from_center_size(icon_pos, Vec2::new(20.0, 20.0)),
                            egui::Rect::from_min_max(Pos2::ZERO, Pos2::new(1.0, 1.0)),
                            Color32::WHITE,
                        );
                    }
                }
            }
        }
    }

    // Draw hover tooltip
    if let Some(idx) = hovered_slice {
        if let Some(mouse_pos) = mouse_pos {
            let result = &results[idx];
            let label = result.get_string("label").unwrap_or_default();
            let value = result.get_float("duration").unwrap_or(0.0);
            let tooltip_text = format!(
                "{}\n{} ({:.1}%)",
                label,
                format_duration_hms(value),
                value / total * 100.0
            );
            draw_tooltip(&painter, &tooltip_text, mouse_pos, center.x);
        }
    }
}

/// Draw legend items
/*
fn draw_legend_items(ui: &mut Ui, results: &[QueryResult]) {
    let colors = get_color_palette();

    ui.add_space(10.0);
    for (idx, result) in results.iter().enumerate() {
        let color = colors[idx % colors.len()];
        let label = result.get_string("label").unwrap_or_default();
        let duration = result.get_float("duration").unwrap_or(0.0);

        ui.horizontal(|ui| {
            let (_response, painter) =
                ui.allocate_painter(Vec2::new(15.0, 15.0), egui::Sense::hover());
            painter.rect_filled(
                egui::Rect::from_min_size(painter.clip_rect().min, Vec2::new(15.0, 15.0)),
                0.0,
                color,
            );

            let time_str = format_duration_hms(duration);
            ui.label(format!("{} - {}", label, time_str));
        });
    }
}
*/

pub fn draw_bar_chart(ui: &mut Ui, results: &[QueryResult]) {
    if results.is_empty() {
        ui.label("No data available");
        return;
    }

    let available_width = ui.available_width();
    let available_height = 400.0_f32.min(ui.available_height());

    let max_value = results
        .iter()
        .filter_map(|r| {
            r.get_float("value")
                .or_else(|| r.get_float("total_time"))
                .or_else(|| r.get_float("duration"))
        })
        .fold(f64::MIN, f64::max);

    if max_value == 0.0 {
        ui.label("No data available");
        return;
    }

    let bar_width = available_width / (results.len() as f32 + 1.0);
    let scale = available_height / max_value as f32;

    let painter = ui.painter();
    let rect_min = ui.cursor().min;
    let rect_max = rect_min + Vec2::new(available_width, available_height);

    painter.line_segment(
        [rect_min, Pos2::new(rect_min.x, rect_max.y)],
        (1.0, Color32::WHITE),
    );
    painter.line_segment(
        [Pos2::new(rect_min.x, rect_max.y), rect_max],
        (1.0, Color32::WHITE),
    );

    let mouse_pos = ui.input(|i| i.pointer.hover_pos());
    let mut hovered_bar: Option<usize> = None;

    if let Some(pos) = mouse_pos {
        for (idx, _) in results.iter().enumerate() {
            let x = rect_min.x + (idx as f32 + 1.0) * bar_width;
            if pos.x >= x
                && pos.x <= x + bar_width * 0.8
                && pos.y >= rect_min.y
                && pos.y <= rect_max.y
            {
                hovered_bar = Some(idx);
                break;
            }
        }
    }

    for (idx, result) in results.iter().enumerate() {
        let value = result
            .get_float("value")
            .or_else(|| result.get_float("total_time"))
            .or_else(|| result.get_float("duration"))
            .unwrap_or(0.0);

        let x = rect_min.x + (idx as f32 + 1.0) * bar_width;
        let bar_height = (value as f32) * scale;

        let bar_top = Pos2::new(x, rect_max.y - bar_height);
        let bar_bottom = Pos2::new(x + bar_width * 0.8, rect_max.y);

        let bar_color = if hovered_bar == Some(idx) {
            Color32::from_rgb(150, 200, 255)
        } else {
            Color32::from_rgb(100, 150, 255)
        };

        painter.rect_filled(
            egui::Rect::from_two_pos(bar_top, bar_bottom),
            2.0,
            bar_color,
        );

        let label = result
            .get_string("label")
            .or_else(|| result.get_string("app_name"))
            .unwrap_or_default();
        let label = truncate_label(&label, 15);

        painter.text(
            Pos2::new(x + bar_width * 0.4, rect_max.y + 5.0),
            egui::Align2::CENTER_TOP,
            label,
            egui::FontId::monospace(9.0),
            Color32::WHITE,
        );
        painter.text(
            Pos2::new(x + bar_width * 0.4, bar_top.y - 5.0),
            egui::Align2::CENTER_BOTTOM,
            &format!("{:.1}", value),
            egui::FontId::default(),
            Color32::LIGHT_GRAY,
        );
    }

    if let Some(idx) = hovered_bar {
        if let Some(mouse_pos) = mouse_pos {
            let result = &results[idx];
            let label = result
                .get_string("label")
                .or_else(|| result.get_string("app_name"))
                .unwrap_or_default();
            let value = result
                .get_float("value")
                .or_else(|| result.get_float("total_time"))
                .unwrap_or(0.0);
            let tooltip_text = format!("{}\n{}", label, format_duration_hms(value));
            draw_tooltip(&painter, &tooltip_text, mouse_pos, rect_max.x / 2.0);
        }
    }

    ui.add_space(available_height + 30.0);
}

pub fn draw_line_chart(ui: &mut Ui, results: &[QueryResult]) {
    if results.is_empty() {
        ui.label("No data available");
        return;
    }

    let available_width = ui.available_width();
    let available_height = 400.0_f32.min(ui.available_height());

    let max_value = results
        .iter()
        .filter_map(|r| r.get_float("value").or_else(|| r.get_float("total_time")))
        .fold(f64::MIN, f64::max);

    if max_value == 0.0 {
        ui.label("No data available");
        return;
    }

    let painter = ui.painter();
    let rect_min = ui.cursor().min;
    let rect_max = rect_min + Vec2::new(available_width, available_height);

    painter.line_segment(
        [rect_min, Pos2::new(rect_min.x, rect_max.y)],
        (1.0, Color32::WHITE),
    );
    painter.line_segment(
        [Pos2::new(rect_min.x, rect_max.y), rect_max],
        (1.0, Color32::WHITE),
    );

    for i in 0..5 {
        let y_value = (max_value / 4.0) * (i as f64);
        let y_pixel = rect_max.y - ((y_value / max_value) as f32 * available_height);
        painter.line_segment(
            [
                Pos2::new(rect_min.x - 5.0, y_pixel),
                Pos2::new(rect_min.x, y_pixel),
            ],
            (1.0, Color32::GRAY),
        );
        painter.text(
            Pos2::new(rect_min.x - 10.0, y_pixel),
            egui::Align2::RIGHT_CENTER,
            &format!("{:.0}", y_value),
            egui::FontId::default(),
            Color32::GRAY,
        );
    }

    let point_spacing = available_width / (results.len() as f32);
    let mouse_pos = ui.input(|i| i.pointer.hover_pos());
    let mut hovered_point: Option<usize> = None;

    if let Some(pos) = mouse_pos {
        for (idx, _) in results.iter().enumerate() {
            let x = rect_min.x + (idx as f32) * point_spacing;
            let value = results[idx]
                .get_float("value")
                .or_else(|| results[idx].get_float("total_time"))
                .unwrap_or(0.0);
            let y = rect_max.y - ((value / max_value) as f32 * available_height);
            let dist = ((pos.x - x).powi(2) + (pos.y - y).powi(2)).sqrt();
            if dist < 8.0 {
                hovered_point = Some(idx);
                break;
            }
        }
    }

    for i in 0..results.len() - 1 {
        let value1 = results[i]
            .get_float("value")
            .or_else(|| results[i].get_float("total_time"))
            .unwrap_or(0.0);
        let value2 = results[i + 1]
            .get_float("value")
            .or_else(|| results[i + 1].get_float("total_time"))
            .unwrap_or(0.0);

        let x1 = rect_min.x + (i as f32) * point_spacing;
        let y1 = rect_max.y - ((value1 / max_value) as f32 * available_height);
        let x2 = rect_min.x + ((i + 1) as f32) * point_spacing;
        let y2 = rect_max.y - ((value2 / max_value) as f32 * available_height);

        painter.line_segment(
            [Pos2::new(x1, y1), Pos2::new(x2, y2)],
            (2.0, Color32::from_rgb(100, 200, 255)),
        );

        let point_color = if hovered_point == Some(i) {
            Color32::from_rgb(200, 255, 100)
        } else {
            Color32::from_rgb(100, 200, 255)
        };
        painter.circle_filled(Pos2::new(x1, y1), 4.0, point_color);
    }

    let last_value = results[results.len() - 1]
        .get_float("value")
        .or_else(|| results[results.len() - 1].get_float("total_time"))
        .unwrap_or(0.0);
    let last_x = rect_min.x + ((results.len() - 1) as f32) * point_spacing;
    let last_y = rect_max.y - ((last_value / max_value) as f32 * available_height);
    let last_color = if hovered_point == Some(results.len() - 1) {
        Color32::from_rgb(200, 255, 100)
    } else {
        Color32::from_rgb(100, 200, 255)
    };
    painter.circle_filled(Pos2::new(last_x, last_y), 4.0, last_color);

    for (idx, result) in results.iter().enumerate() {
        let x = rect_min.x + (idx as f32) * point_spacing;
        let label = result
            .get_string("label")
            .or_else(|| result.get_string("app_name"))
            .unwrap_or_default();
        let label = truncate_label(&label, 12);
        painter.text(
            Pos2::new(x, rect_max.y + 5.0),
            egui::Align2::CENTER_TOP,
            label,
            egui::FontId::monospace(8.0),
            Color32::WHITE,
        );
    }

    if let Some(idx) = hovered_point {
        if let Some(mouse_pos) = mouse_pos {
            let result = &results[idx];
            let label = result
                .get_string("label")
                .or_else(|| result.get_string("app_name"))
                .unwrap_or_default();
            let value = result
                .get_float("value")
                .or_else(|| result.get_float("total_time"))
                .unwrap_or(0.0);
            let tooltip_text = format!("{}\n{}", label, format_duration_hms(value));
            draw_tooltip(&painter, &tooltip_text, mouse_pos, rect_max.x / 2.0);
        }
    }

    ui.add_space(available_height + 30.0);
}

pub fn draw_time_series_chart(ui: &mut Ui, results: &[QueryResult]) {
    if results.is_empty() {
        ui.label("No data available");
        return;
    }

    let available_width = ui.available_width();
    let available_height = 400.0_f32.min(ui.available_height());

    let max_value = results
        .iter()
        .filter_map(|r| r.get_float("value").or_else(|| r.get_float("total_time")))
        .fold(f64::MIN, f64::max);

    if max_value == 0.0 {
        ui.label("No data available");
        return;
    }

    let painter = ui.painter();
    let rect_min = ui.cursor().min;
    let rect_max = rect_min + Vec2::new(available_width, available_height);

    painter.line_segment(
        [rect_min, Pos2::new(rect_min.x, rect_max.y)],
        (1.0, Color32::WHITE),
    );
    painter.line_segment(
        [Pos2::new(rect_min.x, rect_max.y), rect_max],
        (1.0, Color32::WHITE),
    );

    let point_spacing = available_width / (results.len() as f32 + 1.0);
    let mouse_pos = ui.input(|i| i.pointer.hover_pos());
    let mut hovered_bar: Option<usize> = None;

    if let Some(pos) = mouse_pos {
        for (idx, _) in results.iter().enumerate() {
            let x = rect_min.x + (idx as f32 + 1.0) * point_spacing;
            if pos.x >= x && pos.x <= x + point_spacing * 0.8 {
                hovered_bar = Some(idx);
                break;
            }
        }
    }

    for (idx, result) in results.iter().enumerate() {
        let value = result
            .get_float("value")
            .or_else(|| result.get_float("total_time"))
            .unwrap_or(0.0);
        let x = rect_min.x + (idx as f32 + 1.0) * point_spacing;
        let bar_height = (value as f32 / max_value as f32) * available_height;

        let bar_top = Pos2::new(x, rect_max.y - bar_height);
        let bar_bottom = Pos2::new(x + point_spacing * 0.8, rect_max.y);

        let bar_color = if hovered_bar == Some(idx) {
            Color32::from_rgb(200, 255, 100)
        } else {
            Color32::from_rgb(150, 200, 100)
        };

        painter.rect_filled(
            egui::Rect::from_two_pos(bar_top, bar_bottom),
            2.0,
            bar_color,
        );

        let label = result
            .get_string("label")
            .or_else(|| result.get_string("app_name"))
            .unwrap_or_default();
        let label = truncate_label(&label, 12);
        painter.text(
            Pos2::new(x + point_spacing * 0.4, rect_max.y + 5.0),
            egui::Align2::CENTER_TOP,
            label,
            egui::FontId::monospace(8.0),
            Color32::WHITE,
        );
        painter.text(
            Pos2::new(x + point_spacing * 0.4, bar_top.y - 5.0),
            egui::Align2::CENTER_BOTTOM,
            &format!("{:.1}h", value),
            egui::FontId::default(),
            Color32::LIGHT_GRAY,
        );
    }

    if let Some(idx) = hovered_bar {
        if let Some(mouse_pos) = mouse_pos {
            let result = &results[idx];
            let label = result
                .get_string("label")
                .or_else(|| result.get_string("app_name"))
                .unwrap_or_default();
            let value = result
                .get_float("value")
                .or_else(|| result.get_float("total_time"))
                .unwrap_or(0.0);
            let tooltip_text = format!("{}\n{}", label, format_duration_hms(value));
            draw_tooltip(&painter, &tooltip_text, mouse_pos, rect_max.x / 2.0);
        }
    }

    ui.add_space(available_height + 30.0);
}

pub fn draw_results_table_content(ui: &mut Ui, results: &[QueryResult], column_names: &[String]) {
    ui.horizontal(|ui| {
        for name in column_names {
            ui.label(egui::RichText::new(name).strong().size(12.0));
            ui.separator();
        }
    });

    ui.separator();

    for (row_idx, result) in results.iter().enumerate() {
        let row_bg = if row_idx % 2 == 0 {
            Color32::from_gray(10)
        } else {
            Color32::from_gray(30)
        };

        egui::Frame::none().fill(row_bg).show(ui, |ui| {
            ui.set_min_width(ui.available_width());
            ui.horizontal(|ui| {
                for col_name in column_names {
                    let value_str = if let Some(s) = result.get_string(col_name) {
                        truncate_label(&s, MAX_STR_CHARS).to_string()
                    } else if let Some(f) = result.get_float(col_name) {
                        format!("{:.2}", f)
                    } else if let Some(i) = result.get_integer(col_name) {
                        format!("{}", i)
                    } else {
                        "N/A".to_string()
                    };

                    ui.label(value_str);
                    ui.separator();
                }
                ui.add_space(ui.available_width());
            });
        });
    }

    ui.painter()
        .rect_filled(ui.available_rect_before_wrap(), 0.0, Color32::from_gray(15));
}

/// Draw table with ScrollArea wrapper
pub fn draw_results_table(ui: &mut Ui, results: &[QueryResult], column_names: &[String]) {
    ScrollArea::both().auto_shrink([false; 2]).show(ui, |ui| {
        draw_results_table_content(ui, results, column_names);
    });
}

/*pub fn draw_results_table_content(
    ui: &mut Ui,
    results: &[QueryResult],
    column_names: &[String],
    sorted_column: Option<&String>,
    sort_ascending: bool,
) -> Option<String> {
    const COLUMN_WIDTH: f32 = 150.0;
    let mut clicked_column: Option<String> = None;

    // Header with sorting
    ui.horizontal(|ui| {
        for name in column_names {
            let is_sorted = sorted_column.map(|c| c == name).unwrap_or(false);
            let sort_indicator = if is_sorted {
                if sort_ascending {
                    "▲"
                } else {
                    "▼"
                }
            } else {
                ""
            };

            if ui.button(format!("{} {}", name, sort_indicator)).clicked() {
                println!("CLICKED?: {} {}", name, sort_indicator);
                clicked_column = Some(name.clone());
            }
            ui.add_space(COLUMN_WIDTH - 150.0);
        }
    });

    ui.separator();

    // Rows
    for (row_idx, result) in results.iter().enumerate() {
        let row_bg = if row_idx % 2 == 0 {
            Color32::from_gray(10)
        } else {
            Color32::from_gray(30)
        };

        egui::Frame::none().fill(row_bg).show(ui, |ui| {
            ui.set_min_width(ui.available_width());
            ui.horizontal(|ui| {
                for col_name in column_names {
                    let value_str = if let Some(s) = result.get_string(col_name) {
                        truncate_label(&s, 70).to_string()
                    } else if let Some(f) = result.get_float(col_name) {
                        format!("{:.2}", f)
                    } else if let Some(i) = result.get_integer(col_name) {
                        format!("{}", i)
                    } else {
                        "N/A".to_string()
                    };

                    ui.label(value_str);
                    ui.allocate_space(Vec2::new(COLUMN_WIDTH - 150.0, 0.0));
                }
                ui.add_space(ui.available_width());
            });
        });
    }

    ui.painter()
        .rect_filled(ui.available_rect_before_wrap(), 0.0, Color32::from_gray(15));

    clicked_column
}

pub fn draw_results_table(
    ui: &mut Ui,
    results: &[QueryResult],
    column_names: &[String],
    sorted_column: Option<&String>,
    sort_ascending: bool,
) -> Option<String> {
    ui.label(format!("Results: {} rows", results.len()));

    ScrollArea::both()
        .auto_shrink([false; 2])
        .show(ui, |ui| {
            draw_results_table_content(ui, results, column_names, sorted_column, sort_ascending)
        })
        .inner
}*/
