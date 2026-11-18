CREATE TABLE trackz
(
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    -- the actual name of the executable, for example: `gxmessage` or `lxterminal` 
    process_name VARCHAR(256) NOT NULL, 
    -- `app_name` will often be the same as `process_name` but not always. 
    -- For example, on Debain, the process_name for Firefox will be `firefox-esr` 
    -- (assuming that you've installed it the official package) and the app_name will be `Firefox`
    app_name VARCHAR(256) NOT NULL, 
    -- the title of the window (i.e "jessp01/devilspie2: Devilspie2 is an X window (Lua) hooks mechanism — Mozilla Firefox")
    window_name VARCHAR(256) NOT NULL,
    -- start time of focus (for this particular event, not necessarily process launch time!)
    -- stored as UNIX epoch timestamps
    focus_start_time INTEGER NOT NULL,
    -- end time of focus for this particular event (i.e when a different window came to focus)
    focus_end_time INTEGER,
    -- the user who own(s|ed) the process
    process_owner varchar(128)
);
