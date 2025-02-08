# Trackz
Monitor time spent in X apps/windows

## Intended use

- Keeping track of how one's time is spent (working on project X, playing game Y, etc)
- Filing billing reports (for contractors, professional services, etc)

**This is not Intended as a tool for companies to spy on their employees. Whilst I cannot prevent that sort of usage, if
you have that in mind, know that I do not like you. To put it mildly.**

## How does it work?
Trackz makes use of [devilspie 2](https://github.com/jessp01/devilspie2/tree/get-process-owner) to set up hooks 
for window focus and window name/title changes.

These events are then inserted into an SQLite3 DB, allowing you to query it for time spent in a given app/tab/window. Of
course, the data can then also be visualised or exported to CSV, etc.

**Note: the above GIT repo is a fork of the original [devilspie2](https://www.nongnu.org/devilspie2/). 
The code in the `get-process-owner` branch of this fork is needed to support logging the process owner (pull submitted
upstream [here](https://github.com/dsalt/devilspie2/pull/39))** 

**Note II: Trackz was tested predominantly on Debian GNU/Linux but should work on any system where Devilspie 2 and
SQLite3 can run.**

## Installation and setup

### `Devilspie2`

This is a straightforward `make && make install` sort of deployment; there are a few dependencies (as is usually the
case). See [installation instructions](https://github.com/jessp01/devilspie2/blob/implement-get-process-owner/INSTALL#L9) for details.

### `SQLite3`
As noted above, you'll need to install the SQLite3 library and headers. Installing the `sqlite3` CLI util is also
recommended. These packages exist in the official repos of all common distros. On Debian GNU/Linux or Ubuntu, installing the
following should be enough: `libsqlite3-dev`, `sqlite3`

As `Devilspie2`'s hooks are implemented in Lua, you'll also want to install the `lsqlite3` Lua package. You can do that
with:
```sh
# luarocks install lsqlite3
```

#### Creating the DB
Simply run:
```sh
$ sqlite3 /path/to/trackz.db < trackz_schema.sql
```
The dir where `trackz.db` resides needs to be owned by the user who will be doing the writing and that user will of
course need write permissions on `trackz.db`. To better illustrate, if the DB resides in `/etc/trackz/trackz.db` and the
user `devilspie2` executable will run as is `devilspie` then the below should be enough (assuming you use a reasonable `umask`,
if you've got something "special" going, issue the relevant `chomd` commands):
```sh
# chown devilspie /etc/trackz /etc/trackz/trackz.db
```

### Setting up the hooks
See the [config section](https://github.com/jessp01/devilspie2/?tab=readme-ov-file#config) for a general explanation of
how `Devilspie2` works.
The default hooks directory is `~/.config/devilspie2`. 
If you're the only user whose activity you wish to record, that's a good choice. If you wish to record the activity of
multiple users, I'd suggest `/etc/devilspie2` (remember to launch `devilspie2` with `-f /path/to/hooks dir` if you use
anything but the default).


Copy the following files under the `devilspie2` dir to your hooks dir:
- `db.lua`: common code to handle DB insertion and update statements for both hooks 
- `devilspie2.lua`: hook config file 
- `focus_hook.lua`: triggered when an X window gets focus
- `window_name_hook.lua`: triggered when the window title changes

### Setting the ENV vars

- `TRACKZ_DB`: points to the location of the SQLite3 DB
- `XDG_RUNTIME_DIR`: this is typically set to `/run/user/$UID`, it is used to store the last event ID (trackz.id)
  inserted

### Testing

After setting up the DB, hooks and ENV vars as per the above, invoking `devilspie2 -f /etc/devilspie2 --debug` from 
your shell and switching between windows, should result in output similar to this:
```
08/02/2025 05:53:56pm title hook::insert window 'Go Report Card | Go project code quality report cards — Mozilla Firefox (firefox-esr)' Owner: 'jesse'
08/02/2025 05:53:57pm title hook::insert window 'Debugging HTTP Client requests with Go · Jamie Tanna | Software Engineer — Mozilla Firefox (firefox-esr)' Owner: 'jesse'
08/02/2025 05:54:02pm title hook::insert window 'solworktech/trackz: Monitor your X app usage — Mozilla Firefox (firefox-esr)' Owner: 'jesse'
08/02/2025 05:54:04pm title hook::update window 'devilspie2 (lxterminal) ' Owner: 'jesse'
```

If all went well and the output includes no errors, you can use the `sqlite3` CLI client to make sure the data is being
populated:

```sql
$ sqlite3 /etc/trackz/trackz.db
sqlite> .mode box --wrap 40 -- makes the output easier to read, IMHO
sqlite> SELECT id, process_name, window_name,
time(focus_end_time - focus_start_time,'unixepoch') as duration from trackz;
```

### Data schema and notes

The [schema file](./trackz_schema.sql) includes annotations per field, take a look to better understand it.

`trackz` records have a `focus_start_time` column and a `focus_end_time`. These values are stored as UNIX epoch
timestamps. 

**Note that `focus_start_time` is specific to a record/event; it's when the window received focus, not necessarily when the
process was launched. Each process will likely result in multiple records, as you toggle between windows (and tabs).**

#### Useful queries
Output the event ID (auto incremented), process name, window name and the time it spent in focus
(formatted as %H:%M:%S, i.e. 00:01:42):

```sql
SELECT id, process_name, window_name, 
time(focus_end_time - focus_start_time,'unixepoch') as duration from trackz;
```

Sample output:

```
┌────┬──────────────┬────────────────────────────────────────────────────┬──────────┐
│ id │ process_name │                    window_name                     │ duration │
├────┼──────────────┼────────────────────────────────────────────────────┼──────────┤
│ 1  │ lxterminal   │ devilspie2                                         │ 00:00:02 │
├────┼──────────────┼────────────────────────────────────────────────────┼──────────┤
│ 2  │ firefox-esr  │ jessp01/devilspie2: Devilspie2 is an X window (Lua │ 00:01:37 │
│    │              │ ) hooks mechanism; it supports the following event │          │
│    │              │ s: window opened, closed, focused and title change │          │
│    │              │ d — Mozilla Firefox                                │          │
├────┼──────────────┼────────────────────────────────────────────────────┼──────────┤
│ 3  │ firefox-esr  │ Recruitment - overhaul required, urgently (part II │ 00:00:17 │
│    │              │ ) - Jesse Portnoy — Mozilla Firefox                │          │
├────┼──────────────┼────────────────────────────────────────────────────┼──────────┤
│ 4  │ firefox-esr  │ Watch The Big Bang Theory - Season 6 | Prime Video │ 00:02:05 │
│    │              │  — Mozilla Firefox                                 │          │
└────┴──────────────┴────────────────────────────────────────────────────┴──────────┘

```

Output all events where the process name is `firefox-esr`, include event duration (formatted as `%H:%M:%S`) and the
focus end time (in localtime), order by focus duration:

```sql
SELECT id, process_name, window_name, 
time (focus_end_time - focus_start_time,'unixepoch') as duration, 
DATETIME(ROUND(focus_end_time), 'unixepoch','localtime') as focus_end_time 
from trackz where process_name='firefox-esr' 
order by duration desc;
```

Sample output:

```
┌─────┬──────────────┬────────────────────────────────┬──────────┬─────────────────────┐
│ id  │ process_name │          window_name           │ duration │   focus_end_time    │
├─────┼──────────────┼────────────────────────────────┼──────────┼─────────────────────┤
│ 34  │ firefox-esr  │ Watch The Big Bang Theory - Se │ 00:00:58 │ 2025-02-08 19:22:52 │
│     │              │ ason 6 | Prime Video — Mozilla │          │                     │
│     │              │  Firefox                       │          │                     │
├─────┼──────────────┼────────────────────────────────┼──────────┼─────────────────────┤
│ 38  │ firefox-esr  │ jessp01 (Jesse Portnoy) — Mozi │ 00:00:49 │ 2025-02-08 19:28:13 │
│     │              │ lla Firefox                    │          │                     │
├─────┼──────────────┼────────────────────────────────┼──────────┼─────────────────────┤
│ 37  │ firefox-esr  │ Notifications | LinkedIn — Moz │ 00:00:41 │ 2025-02-08 19:27:24 │
│     │              │ illa Firefox                   │          │                     │
├─────┼──────────────┼────────────────────────────────┼──────────┼─────────────────────┤
│ 149 │ firefox-esr  │ Command Line Shell For SQLite  │ 00:00:01 │ 2025-02-08 20:05:45 │
│     │              │ — Mozilla Firefox              │          │                     │
└─────┴──────────────┴────────────────────────────────┴──────────┴─────────────────────┘

```

Aggregate events by `process_name`, `window_name` and day (calculated from `focus_start_time`), 
order by day and total (aggregated) duration.

This is probably one of the most useful examples as you can use it to see where most of your time was spent.

```sql
SELECT DISTINCT process_name, window_name, 
time (sum (focus_end_time - focus_start_time),'unixepoch') as focus_duration, 
sum (focus_end_time - focus_start_time) as focus_in_seconds, 
strftime('%d-%m-%Y', datetime(focus_start_time, 'unixepoch')) as day from trackz 
group by process_name, window_name, day order by day, sum (focus_end_time - focus_start_time);
```

Sample output:

```
┌──────────────┬────────────────────────────────┬────────────────┬───────────────┬────────────┐
│ process_name │          window_name           │ focus_duration │ focus_in_secs │    day     │
├──────────────┼────────────────────────────────┼────────────────┼───────────────┼────────────┤
│ lxterminal   │ jesse@jessex: ~/tmp/trackz     │ 00:43:37       │ 2617          │ 08-02-2025 │
├──────────────┼────────────────────────────────┼────────────────┼───────────────┼────────────┤
│ lxterminal   │ root@jessex: ~                 │ 00:39:20       │ 2360          │ 08-02-2025 │
├──────────────┼────────────────────────────────┼────────────────┼───────────────┼────────────┤
│ firefox-esr  │ Watch The Big Bang Theory - Se │ 00:29:17       │ 1757          │ 08-02-2025 │
│              │ ason 6 | Prime Video — Mozilla │                │               │            │
│              │  Firefox                       │                │               │            │
├──────────────┼────────────────────────────────┼────────────────┼───────────────┼────────────┤
│ gxmessage    │ Food run                       │ 00:26:12       │ 1572          │ 08-02-2025 │
├──────────────┼────────────────────────────────┼────────────────┼───────────────┼────────────┤
│ kblocks      │ KBlocks                        │ 00:13:56       │ 836           │ 08-02-2025 │
└──────────────┴────────────────────────────────┴────────────────┴───────────────┴────────────┘

```

### Running Trackz automatically on system init/session init

`devilspie2` needs to run as a user that has access to the X display.
If you're the only user likely to start X sessions on the machine, simply edit 
[devilspie2/devilspie2.service](./devilspie2/devilspie2.service) and set the `User` directive to your own user.
If you sometimes initiate sessions for other users by invoking `su -` from your X terminal emulator and then 
launch X apps from that TTY, it will work and `trackz.process_owner` will be set to the correct user. 

See [devilspie2/devilspie2.service](./devilspie2/devilspie2.service) and
[devilspie2/devilspie2.env](./devilspie2/devilspie2.env). The location of `devilspie2.service` may vary depending on
your distro of choice but will likely be `/usr/lib/systemd/system/devilspie2.service`. Check your distro's documentation if you're unsure.

Start the service with:

```sh
# systemctl start devilspie2
```

If you want it to automatically launch at system init:

```sh
systemctl enable devilspie2
```

### Note on running the service as a user that does not initiate an X session

You can use the below shell script (the examples all assume you place it under 
`/usr/local/bin/xauth_setup.sh`) but I should note that it's somewhat hackish:

```sh
#!/bin/sh -e
NORMAL_X_USER=@YOUR_USER_HERE@
DEVILSPIE_USER=devilspie # you can change to any user you wish, so long as it's a valid one
DISPLAY=:0.0 # change as needed 
X_AUTH_LIST=$(su - $NORMAL_X_USER -c 'xauth list|grep "`uname -n`/unix:0"')
X_MAGIC_COOKIE=$(echo $X_AUTH_LIST|awk -F " " '{print $NF}')
su - $DEVILSPIE_USER -c "xauth add $DISPLAY .  $X_MAGIC_COOKIE"
```

You will also need this little C wrapper:
```c
#include <stdlib.h>
#include <unistd.h>

int main()
{
    setuid(0);
    int rc = system("/usr/local/bin/xauth_setup.sh");
    return rc;
}
```

Compile the wrapper with `$CC /usr/local/bin/xauth_setup.c -o /usr/local/bin/xauth_setup` (where `$CC` is set to whatever
C compiler you use - gcc, clang, etc)

Finally, comment out the existing `ExecStartPre` directive in [devilspie2/devilspie2.service](./devilspie2/devilspie2.service) and replace it with `ExecStartPre=/usr/local/bin/xauth_setup && rm -f ${XDG_RUNTIME_DIR}/trackz_last_event_id`
