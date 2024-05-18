-- select id,process_name, window_name, time(focus_end_time - focus_start_time,'unixepoch') AS duration   from trackz;
-- select distinct process_name, window_name, time (sum (focus_end_time - focus_start_time),'unixepoch') as time from trackz group by process_name, window_name order by sum (focus_end_time - focus_start_time);
-- select  process_name, window_name, time (focus_end_time - focus_start_time,'unixepoch') as time, process_name, app_name, window_name, time (focus_end_time - focus_start_time,'unixepoch') as time, DATETIME(ROUND(focus_end_time), 'unixepoch','localtime') as date from trackz where process_name='firefox-esr' ;
db_path = '/home/jesse/tmp/trackz-local/trackz.db' 
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
    local last_event_record = nil

    local last_event_id_handle = io.open(last_event_id_file,"r")
    local last_event_id = last_event_id_handle:read()
    last_event_id_handle:close()
    if last_event_id then
	-- local time = os.date("%Y-%m-%d %I:%M:%S")
	local stmt = db:prepare("SELECT process_name, window_name FROM trackz WHERE id = ?")
	stmt:bind_values(last_event_id)
	res = stmt:step()
	if res == sqlite3.ROW then
	    last_event_record = stmt:get_named_values()
	end
	stmt:finalize()
	-- debug_print(os.date("%d/%m/%Y %I:%M:%S %p") .. " title hook:: LAST Window Name: " .. last_event_record.window_name);
	if (last_event_record ~= nil and last_event_record ~= '') and (last_event_record.window_name ~= window_name or last_event_record.process_name ~= app_name) then 
	    debug_print(os.date("%d/%m/%Y %I:%M:%S %p") .. " title hook:: Window Name: " .. window_name);
	    local time = os.time(os.date("!*t"))
	    local stmt = db:prepare[[ UPDATE trackz set focus_end_time = ? where id = ?]]
	    stmt:bind_values(time, last_event_id)
	    stmt:step()
	    stmt:finalize()
	end
    end

    if last_event_record == nil or (last_event_record.window_name ~= window_name or last_event_record.process_name ~= app_name) then 
	local time = os.time(os.date("!*t"))
	local stmt = db:prepare[[ INSERT INTO trackz VALUES (:id, :process_name, :app_name, :window_name, :focus_start_time, :focus_end_time) ]]
	stmt:bind_names{id = nil,  process_name = process_name,  app_name = app_name, window_name = window_name, focus_start_time = time }
	stmt:step()
	stmt:finalize()
	last_event_id_handle = io.open(last_event_id_file,"w")
	event_id = last_event_id_handle:read()
	last_event_id_handle:write(db:last_insert_rowid())
	last_event_id_handle:close()
	db:close()
    end

end
