use std::fs;
use std::path::PathBuf;

pub struct IconResolver;

impl IconResolver {
    pub fn get_icon(app_name: &str) -> Option<String> {
        let home_paths = &format!(
            "{}/.local/share/applications",
            std::env::var("HOME").unwrap_or_default()
        );
        let search_paths = vec![
            "/usr/share/applications",
            "/usr/local/share/applications",
            home_paths,
        ];

        for search_path in search_paths {
            if let Ok(entries) = fs::read_dir(search_path) {
                for entry in entries.flatten() {
                    let path = entry.path();
                    if path.extension().map(|e| e == "desktop").unwrap_or(false) {
                        if let Ok(content) = fs::read_to_string(&path) {
                            // Simple text parsing for Icon= line
                            for line in content.lines() {
                                if line.starts_with("Exec=") && line.contains(app_name) {
                                    // Found matching desktop file, now get Icon
                                    for icon_line in content.lines() {
                                        if icon_line.starts_with("Icon=") {
                                            return Some(
                                                icon_line.replace("Icon=", "").trim().to_string(),
                                            );
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        None
    }

    pub fn get_icon_path(icon_name: &str, sizes: &[&str]) -> Option<String> {
        // Look in icon theme directories
        let home_paths = &format!(
            "{}/.local/share/icons",
            std::env::var("HOME").unwrap_or_default()
        );
        let icon_dirs = vec![
            "/usr/share/icons/hicolor",
            "/usr/share/pixmaps",
            "/usr/share/icons/HighContrast",
            home_paths,
        ];

        for icon_dir in icon_dirs {
            // Try common sizes
            for size in sizes {
                for ext in &["png", "svg"] {
                    let path = format!("{}/{}/apps/{}.{}", icon_dir, size, icon_name, ext);
                    if PathBuf::from(&path).exists() {
                        return Some(path);
                    }
                }
            }
        }
        None
    }
}
