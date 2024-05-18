CREATE TABLE trackz
(
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    process_name VARCHAR(256) NOT NULL,
    app_name VARCHAR(256) NOT NULL,
    window_name VARCHAR(256) NOT NULL,
    focus_start_time INTEGER NOT NULL,
    focus_end_time INTEGER
);
