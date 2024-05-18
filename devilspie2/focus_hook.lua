require "db"
local process_name = get_process_name()
local application_name = get_application_name()
local window_name = get_window_name()

if process_name ~= nil and process_name ~= '' then
    insert_to_db(process_name, app_name, window_name, focus_start_time)
    debug_print(os.date("%d/%m/%Y %I:%M %p") .. " -- focus hook:: Process name: " .. process_name)
end

