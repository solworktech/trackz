-- select id,process_name, window_name, time(focus_end_time - focus_start_time,'unixepoch') AS duration   from trackz;
-- select distinct process_name, window_name, time (sum (focus_end_time - focus_start_time),'unixepoch') as time from trackz group by process_name, window_name order by sum (focus_end_time - focus_start_time);
db_path = '/home/jesse/tmp/trackz/trackz.db' 
runtime_dir = os.getenv('XDG_RUNTIME_DIR')
last_event_id_file = runtime_dir .. '/trackz_last_event_id'
local last_event_id_handle = io.open(last_event_id_file,"r")
if not last_event_id_handle then
    last_event_id_handle = io.open(last_event_id_file,"w")
end
last_event_id_handle:close()
local sqlite3 = require("lsqlite3")
function insert_to_db(process_name, app_name, window_name, focus_start_time)
    local db = sqlite3.open(db_path, sqlite3.OPEN_READWRITE)

    local last_event_id_handle = io.open(last_event_id_file,"r")
    local last_event_id = last_event_id_handle:read()
    last_event_id_handle:close()
    if last_event_id then
	local time = os.time(os.date("!*t"))
	-- local time = os.date("%Y-%m-%d %I:%M:%S")
	-- print (time)
        local stmt = db:prepare[[ UPDATE trackz set focus_end_time = ? where id = ?]]
	-- print ('UPDATE trackz set focus_end_time = ' .. time .. ' where id =' .. tonumber(last_event_id))
	-- stmt:bind_values(os.date(), last_event_id)
	stmt:bind_values(time, last_event_id)
	stmt:step()
	stmt:finalize()
    end

    local stmt = db:prepare[[ INSERT INTO trackz VALUES (:id, :process_name, :app_name, :window_name, :focus_start_time, :focus_end_time) ]]

    local time = os.time(os.date("!*t"))
    stmt:bind_names{id = nil,  process_name = process_name,  app_name = app_name, window_name = window_name, focus_start_time = time }
    -- stmt:bind_names{id = nil,  process_name = process_name,  app_name = app_name, window_name = window_name, focus_start_time = os.time(os.date("!*t")) }
    stmt:step()
    stmt:finalize()
    last_event_id_handle = io.open(last_event_id_file,"w")
    event_id = last_event_id_handle:read()
    last_event_id_handle:write(db:last_insert_rowid())
    last_event_id_handle:close()
    db:close()
end
