use egui::{Align2, Color32, FontId, Painter, Pos2, Vec2};
use std::env;
use std::path::PathBuf;

// Tooltip styling constants
pub const TOOLTIP_BG_COLOR: Color32 = Color32::from_rgb(20, 20, 20);
pub const TOOLTIP_TEXT_COLOR: Color32 = Color32::from_rgb(255, 255, 100);
pub const TOOLTIP_PADDING: f32 = 8.0;

/// Get the database path from environment or default to cwd
pub fn get_db_path() -> PathBuf {
    env::var("TRACKZ_DB")
        .map(PathBuf::from)
        .unwrap_or_else(|_| PathBuf::from("trackz.db"))
}

/// Truncate label safely without panicking on UTF-8 boundaries
pub fn truncate_label(s: &str, max_chars: usize) -> &str {
    match s.char_indices().nth(max_chars) {
        None => s,
        Some((idx, _)) => &s[..idx],
    }
}

/// Format duration in HH:mm:ss format
pub fn format_duration_hms(hours: f64) -> String {
    let total_seconds = (hours * 3600.0) as i32;
    let h = total_seconds / 3600;
    let m = (total_seconds % 3600) / 60;
    let s = total_seconds % 60;
    format!("{:02}:{:02}:{:02}", h, m, s)
}

/// Format duration in human-readable form (minutes/hours/days)
/*pub fn format_duration(hours: f64) -> String {
    if hours < 1.0 {
        format!("{:.0}m", hours * 60.0)
    } else if hours < 24.0 {
        format!("{:.1}h", hours)
    } else {
        format!("{:.1}d", hours / 24.0)
    }
}*/

/// Generate a palette of visually distinct dark colours using HSL colour space
pub fn get_color_palette() -> Vec<Color32> {
    const NUM_COLORS: usize = 20;
    const SATURATION: f32 = 0.85; // 85% saturation for vibrant colours
    const LIGHTNESS: f32 = 0.45; // 45% lightness for dark, high-contrast colours

    (0..NUM_COLORS)
        .map(|i| {
            let hue = (i as f32 / NUM_COLORS as f32) * 360.0; // Distribute evenly around colour wheel
            hsl_to_rgb(hue, SATURATION, LIGHTNESS)
        })
        .collect()
}

/// Convert HSL to RGB colour
fn hsl_to_rgb(hue: f32, saturation: f32, lightness: f32) -> Color32 {
    let h = hue / 60.0;
    let c = (1.0 - (2.0 * lightness - 1.0).abs()) * saturation;
    let x = c * (1.0 - ((h % 2.0) - 1.0).abs());
    let m = lightness - c / 2.0;

    let (r, g, b) = match h as u32 {
        0 => (c, x, 0.0),
        1 => (x, c, 0.0),
        2 => (0.0, c, x),
        3 => (0.0, x, c),
        4 => (x, 0.0, c),
        5 => (c, 0.0, x),
        _ => (0.0, 0.0, 0.0),
    };

    Color32::from_rgb(
        ((r + m) * 255.0) as u8,
        ((g + m) * 255.0) as u8,
        ((b + m) * 255.0) as u8,
    )
}

/// Draw a single tooltip with dark background - width calculated from text length
pub fn draw_tooltip(painter: &Painter, text: &str, mouse_pos: Pos2, rect_center_x: f32) {
    // Estimate width based on text length (rough: ~8 pixels per character at 11pt)
    let text_width = (text.len() as f32 * 4.0).max(100.0);
    let tooltip_width = text_width + TOOLTIP_PADDING * 2.0;

    // Calculate height based on line count
    let line_count = text.matches('\n').count() + 1;
    let line_height = 15.0;
    let tooltip_height = (line_count as f32 * line_height) + TOOLTIP_PADDING * 2.0;

    // Smart positioning: if near right edge, draw to the left of cursor
    let tooltip_offset_x = if mouse_pos.x > rect_center_x {
        -tooltip_width - 10.0
    } else {
        10.0
    };

    let tooltip_pos = mouse_pos + Vec2::new(tooltip_offset_x, 10.0);

    // Draw dark background
    painter.rect_filled(
        egui::Rect::from_min_size(tooltip_pos, Vec2::new(tooltip_width, tooltip_height)),
        3.0,
        TOOLTIP_BG_COLOR,
    );

    // Draw text
    painter.text(
        tooltip_pos + Vec2::new(TOOLTIP_PADDING, TOOLTIP_PADDING),
        Align2::LEFT_TOP,
        text,
        FontId::proportional(11.0),
        TOOLTIP_TEXT_COLOR,
    );
}
