# Trackz
Monitor time spent in X apps/windows

## What does it actually mean?
Trackz makes use of [devilspie2](https://github.com/jessp01/devilspie2/tree/get-process-owner) to set up hooks for window focus and window name/title changes.
**Note: the above GIT repo is a fork of the original [devilspie2](https://www.nongnu.org/devilspie2/). 
The code in the `get-process-owner` branch of this fork is needed to support logging the process owner (pull submitted
upstream [here](https://github.com/dsalt/devilspie2/pull/39)) 

These events are then inserted into an SQLite3 DB, allowing you to query it for time spent in a given app/tab/window. Of
course, the data can then also be visualised or exported to CSV, etc.

## Intended use
- Keeping track of how one's time is spent (working on project X, playing game Y, etc)
- Filing billing reports (for contractors, professional services, etc)

**This is not Intended as a tool for companies to spy on their employees. Whilst I cannot prevent that sort of usage, if
you have that in mind, know that I do not like you. To put it mildly.**

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
user `devilspie2` will run under is `devilspie` then the below should be enough (assuming you use a reasonable `umask`,
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
