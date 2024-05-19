local script_dirname = debug.getinfo (1, 'S').source:match[[^@?(.*[\/])[^\/]-$]];
package.path     = script_dirname .. '/?.lua;'  .. package.path;
require "db"
local process_name = get_process_name()
local process_owner_name = get_process_owner()
local app_name = get_application_name()
local window_name = get_window_name()
if process_name ~= nil and process_name ~= '' then
    insert_to_db(process_name, app_name, window_name, process_owner_name, focus_start_time)
end
