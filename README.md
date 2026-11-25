## [Trackz](https://github.com/solworktech/trackz) - Monitor time spent in apps/windows

[![CI](https://github.com/solworktech/trackz/actions/workflows/ci.yml/badge.svg)](https://github.com/solworktech/trackz/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/solworktech/trackz.svg)](https://pkg.go.dev/github.com/solworktech/trackz)
[![License][badge-license]][license]

## Intended use

- Keeping track of how one's time is spent (working on project X, playing game Y, corresponding with colleague N, etc)
- Filing billing reports (for contractors, professional services, etc)

**This is not Intended as a tool for companies to spy on their employees. Whilst I cannot prevent that sort of usage, if
you have that in mind, know that I do not like you. To put it mildly.**

## How does it work?

Trackz consists of two programs, written in Go:

- **`trackz`**: a small daemon that watches which window has focus and records every focus change and window
  title change in an SQLite3 database: process name, application name, window title, process owner and the time
  the window gained and lost focus. It also records periods when the screen was locked or the machine was asleep.
- **`trackz-viz`**: a graphical dashboard ([Gio](https://gioui.org)) with charts, tables, custom SQL queries and CSV
  export.

Because the process name, application name, window title and the exact time the window was in focus are all recorded,
you can perform granular queries. For example, you could check how long was spent:

- Talking to Jim on Slack yesterday
- On the "Project X" channel this week
- Editing spreadsheets in LibreOffice
- Editing annoying.cvs specifically
- In apps (browser, LibreOffice, terminal, VIM, whatever) where the window title included "Project Z"

See the [Useful Queries](#useful-queries) section for some examples.

### Supported platforms

| Platform | Window tracking | Sleep / lock detection |
|---|---|---|
| Linux, FreeBSD, OpenBSD, NetBSD, DragonFly (X11, or XWayland apps) | [libwnck](https://gitlab.gnome.org/GNOME/libwnck) | D-Bus: `*.ScreenSaver.ActiveChanged` (session), logind `PrepareForSleep` (system) |
| macOS | NSWorkspace + Accessibility API | NSWorkspace sleep/wake/screens, `com.apple.screenIsLocked` |
| Windows | `SetWinEventHook` (foreground and title changes) | `WM_POWERBROADCAST`, `WM_WTSSESSION_CHANGE` |

Notes:

- libwnck only sees X11 windows. On a Wayland session, native Wayland windows are invisible to it (devilspie2 had the
  same limitation); applications running under XWayland are tracked.
- On macOS, window titles require Accessibility permission: allow the `trackz` binary (or the terminal you start it
  from) under *System Settings → Privacy & Security → Accessibility*. Without it, application switches are still
  recorded, with the application name as the window title.
- Only the title changes of the **focused** window are recorded.
- On Windows, explorer's task switching UI (Alt+Tab, Task View) is not recorded: the previous window stays current
  until the switcher activates another one.

**Pro tips**

- If, like me, you do a lot of work in the terminal, be sure to set custom titles to your tabs to denote what you're
  working on (i.e: "project X, component Z", "git clone", etc)
- If you want your reports to include time spent away from the machine, you can use a util like `gxmessage`, for example, to
  record a lunch break, invoke: `gxmessage --title "Food run" "Let's see how long this takes..."` before you leave and
  close it when you return

## Installation

### Packages

Each [release](https://github.com/solworktech/trackz/releases) has:

- **Debian/Ubuntu** (`.deb`) and **Fedora/RHEL/openSUSE** (`.rpm`) packages for amd64 and arm64. They install
  `trackz` and `trackz-viz` to `/usr/bin`, the desktop entry and icons, the systemd user unit and an XDG autostart
  entry, so `trackz` starts with every user's graphical session:
  - The unit is enabled for every user (`systemctl --global enable trackz`). It is wanted by
    `graphical-session.target`, which only some desktops start: the session manager has to, and GNOME's and KDE
    Plasma's do, while Xfce's and MATE's, for example, don't.
  - The autostart entry (`/etc/xdg/autostart/trackz.desktop`) covers the others. It runs `trackz autostart`, which
    exits at once where the unit is enabled and `graphical-session.target` is active, and records otherwise.

  Users in a graphical session during the installation get `trackz` started straight away where the unit applies
  (elsewhere, at their next login). An upgrade restarts the unit where it runs; removal stops it and disables it. A
  user who does not want `trackz` runs `systemctl --user mask trackz`, which `trackz autostart` respects too
  (`disable` does not undo a global enable).
- **Arch Linux** (`.pkg.tar.zst`): the binaries, the desktop entry, icons and the unit; as usual on Arch, nothing
  starts by itself: `systemctl --user enable --now trackz`, or the autostart entry (see [Starting
  automatically](#starting-automatically)).
- **macOS**: `tar.gz` archives (see [Starting automatically](#starting-automatically) for the LaunchAgent).
- **Windows**: the MSI installers (see [Windows installer](#windows-installer)).
- `tar.gz` archives of the Linux binaries, with the unit, desktop entries and icons.

### Requirements

- Go 1.24 or newer.
- **Linux/BSD**: a C compiler, `pkg-config` and the libwnck 3 and GTK 3 development files for `trackz`; for
  `trackz-viz`, Gio's [dependencies](https://gioui.org/doc/install/linux) (Wayland, X11, EGL/GLES and Vulkan
  headers). On Debian/Ubuntu:
  ```sh
  # apt install gcc pkg-config libwnck-3-dev libgtk-3-dev \
        libwayland-dev libx11-dev libx11-xcb-dev libxkbcommon-x11-dev libgles-dev libegl-dev \
        libffi-dev libxcursor-dev libvulkan-dev
  ```
  On FreeBSD: `pkg install go pkgconf libwnck3 gtk3`.
- **macOS**: the Xcode command line tools (`xcode-select --install`).
- **Windows**: nothing beyond Go; no C compiler is needed.

SQLite is built in ([modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), pure Go): no SQLite library or Lua
modules are needed. The `sqlite3` CLI is still handy for ad-hoc queries.

### Building

```sh
$ make            # builds bin/trackz and bin/trackz-viz
# make install    # copies them to /usr/local/bin
```

or `go build ./cmd/trackz ./cmd/trackz-viz`. On Windows, `make` builds both without a console window; use
`go build` directly if you want one for `trackz` (to see its log output).

## Running

```sh
$ trackz run -debug
```

`trackz run` (or just `trackz`) creates the database if needed and starts recording. With `-debug`, every event is
logged:

```
2026/02/08 17:53:56 insert window 'Go Report Card | Go project code quality report cards — Mozilla Firefox (firefox-esr)' Owner: 'jesse'
2026/02/08 17:53:57 update record 41: focus_end_time=1770573237
```

| Flag | Meaning |
|---|---|
| `-db PATH` | database file (see below) |
| `-debug` | log every event |
| `-log FILE` | append the log to FILE instead of stderr |
| `-id-file FILE` | where to mirror the id of the open record (default `$XDG_RUNTIME_DIR/trackz_last_event_id`, `-` to disable) |
| `-no-power` | do not record sleep/lock periods |
| `-dbus-monitor` | Linux/BSD: eavesdrop on the session bus (`BecomeMonitor`) for screen-lock signals, for lockers that do not broadcast `ActiveChanged` |

Other commands: `trackz init-db`, `trackz fix-timestamps` (see [Migrating](#migrating-from-the-devilspie2-version)),
`trackz version`.

Stopping `trackz` (Ctrl-C, `SIGTERM`) closes the open record.

### Database location

`trackz` and `trackz-viz` pick the database the same way, in order of precedence:

1. the `-db` flag, if given on the command line,
2. the `TRACKZ_DB` environment variable,
3. `db_path` in `config.json`,
4. the default, `trackz.db` next to `config.json` in the per-user config directory:
   - Linux/BSD: `$XDG_CONFIG_HOME/trackz/trackz.db`, or `~/.config/trackz/trackz.db` if `XDG_CONFIG_HOME` is unset
   - macOS: `~/Library/Application Support/trackz/trackz.db`
   - Windows: `%APPDATA%\trackz\trackz.db`

A leading `~` and `$VARIABLES` in `TRACKZ_DB`, `db_path` and `-db` are expanded by Trackz itself, so
`TRACKZ_DB=~/data/trackz.db` also works in a systemd `Environment=` line or `EnvironmentFile=` (systemd does not expand
them). `trackz` logs the absolute path it records to, and `trackz-viz` shows the database it opened in its window title.

Note that a `TRACKZ_DB` set only in the systemd unit (or its `EnvironmentFile`) is not seen by `trackz-viz` started from
a shell or the desktop menu. Use the default, `db_path` in `config.json` or an environment variable set for the whole
session so both programs use the same file; `trackz-viz` warns when no `trackz` is recording to the database it shows.

Only one `trackz` can record to a database at a time: a second one exits (with status 3, which the systemd unit does
not restart on) and an error naming the first one's PID. The
lock file is `<database>.lock`; `trackz-viz` also uses it to tell whether `trackz` is running.

`trackz` closes the open record when stopped with Ctrl-C, `SIGTERM`, `SIGHUP` (e.g. its terminal is closed) or
`SIGQUIT`. If it is killed outright or crashes, the next start closes that record at the last moment the previous run
was known to be alive: it writes a heartbeat to `$XDG_RUNTIME_DIR` (the temporary directory elsewhere) on every focus
change and every 30 seconds, so at most 30 seconds are lost.

The directory holding the database must be writable by the user running `trackz`.

### Recording several users

`trackz` runs in the desktop session of the user whose windows it watches; the separate `devilspie` user and the
`xauth` workaround are no longer needed. The process owner is read per window, so if you run `su - otheruser` in a
terminal and start X apps from it, `process_owner` is still that user. To collect several users' activity in one
database, point their `TRACKZ_DB` at a shared file in a directory they can all write to.

### Starting automatically

- **Linux/BSD with systemd**: [packaging/systemd/trackz.service](./packaging/systemd/trackz.service) is a *user* unit
  (the deb and rpm packages install and enable it):
  ```sh
  $ install -Dm644 packaging/systemd/trackz.service ~/.config/systemd/user/trackz.service
  $ systemctl --user enable --now trackz
  ```
  It runs `trackz` from `/usr/local/bin` or `/usr/bin`. Environment overrides (e.g. `TRACKZ_DB`) go in
  `~/.config/trackz/trackz.env`.
- **Any XDG desktop**: copy [packaging/xdg/trackz.desktop](./packaging/xdg/trackz.desktop) to
  `~/.config/autostart/` (or `/etc/xdg/autostart/` for all users). It runs `trackz autostart`, which leaves the
  session to the systemd unit where the unit is enabled and `graphical-session.target` is active (or where the unit
  is masked), so the two can be installed together.
- **macOS**: see [packaging/launchd/org.solworktech.trackz.plist](./packaging/launchd/org.solworktech.trackz.plist).
- **Windows**: the installer (below) offers it; without the installer,
  `powershell -ExecutionPolicy Bypass -File packaging\windows\install-autostart.ps1 -Exe C:\path\to\trackz.exe`.

### Stopping

`trackz stop` (with `-db` if `trackz run` had one) asks the `trackz` recording to that database to stop, and waits
until it has closed the open record and released the database. On Unix it sends `SIGTERM` (which also works by hand,
as do Ctrl-C, `SIGHUP` and `SIGQUIT`); on Windows, where there are no such signals, it sets an event `trackz` waits on.
The systemd unit uses it as `ExecStop`. `trackz-viz -quit` likewise asks a running `trackz-viz` to quit, and waits.

### Windows installer

[packaging/windows/wix](./packaging/windows/wix) builds a per-user MSI with [WiX Toolset](https://wixtoolset.org) v5
(the last version without the Open Source Maintenance Fee). One page shows the licence (AGPLv3), says the installation
is for the current Windows account only, and offers the install folder (default `%LOCALAPPDATA%\Programs\Trackz`),
*Start the Trackz recorder when I sign in to Windows* and *Start Trackz when setup finishes*. No administrator rights
are needed. It adds a *Trackz* Start menu entry for `trackz-viz` and, if chosen, starts `trackz run` at sign-in
(`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`). Upgrades and removal first run `trackz-viz -quit` and
`trackz stop`, so no file is in use and the open record is closed. Uninstalling leaves the database and settings in
`%APPDATA%\trackz`.

The [windows-installer](./.github/workflows/windows-installer.yml) workflow builds x64 and ARM64 MSIs and attaches
them to the release for a `v*` tag. By hand, on Windows with the .NET SDK and the executables in `bin`:
```powershell
PS> dotnet build packaging\windows\wix\Trackz.wixproj -c Release -p:Platform=x64 -p:TrackzVersion=1.2.3
```
The banner, side image and `License.rtf` are generated (`make icons`) from the logo and `LICENSE`.

### Releasing

Pushing a `v*` tag runs the [release](./.github/workflows/release.yml) workflow, with
[GoReleaser](https://goreleaser.com). `trackz` and `trackz-viz` need cgo, so they are built on each OS rather than
cross-compiled from one: [.goreleaser.yaml](./.goreleaser.yaml) on Ubuntu (amd64, and arm64 with the cross compiler and
an arm64 sysroot made in a container) makes the release with the changelog, the Linux archives and the deb, rpm and
Arch packages;
[.goreleaser.darwin.yaml](./.goreleaser.darwin.yaml) on macOS then adds the macOS archives (its Homebrew formula is
commented out for now). The Windows MSIs come from the windows-installer workflow. The deb and rpm scripts are in
[packaging/linux](./packaging/linux). There are no BSD binaries: build from source there.

To try the build locally, for this machine only: `goreleaser build --snapshot --clean --single-target`.

## Visualisation: `trackz-viz`

```sh
$ trackz-viz            # or: trackz-viz -db /path/to/trackz.db
```

- **Time range**: Today, last 7/30/90 days, or custom dates. *Rows* sets how many entries the Dashboard and Top Apps
  show (20 by default).
- **Dashboard**: the top activities (process + window title) as a pie chart with application icons, plus a table of
  Process Name (with the icon), Window Title and Duration. A click on a column title sorts by it (durations by
  length of time, text ignoring case); another click reverses the order. Custom Query tables sort the same way. The
  dashboard is exportable as CSV (to the path in the *Export to* field, which defaults to
  `~/trackz-<view>-<date>.csv`), with the duration also in seconds.
- Pie slices wide enough show the application's icon; hovering over a slice, bar or point shows its details (with the
  icon, for pie slices).
- Reports that include the present refresh themselves every 5 minutes (configurable; 0 turns it off), and the window
  currently in focus is counted up to now. The heading shows how many records were left out for being shorter than the minimum duration.
- **Top Apps** (bar chart), **Time Spent** per app (pie chart), **Window Activity** over time (focus time per hour,
  or per day for longer ranges).
- **Timelines**: in the Dashboard, Top Apps and Time Spent, hovering over an entry also shows when it was first and
  last in focus in the range (times for a single day, dates and times for longer ranges), and clicking it (its slice,
  bar, legend entry or table row) shows its focus time per hour (a single day), per day (up to 31 days) or per week
  (from Monday; longer ranges) in place of the chart, until *Back*. A focus period that spans hours or days counts in
  each for the part in it. *Hide … without focus* leaves out the empty ones. The timeline follows the range and the
  filter, and is what *Export CSV* exports while it is shown.
- **Filter**: *Filter…* opens a dialog to keep only windows whose title contains some text (ignoring case) and to
  exclude applications by unticking them in the list of applications used in the range. It applies to every view but
  Custom Query (and to CSV exports) until cleared. While it is on, the button reads *Filter (on)…* and the heading
  says what is filtered, e.g. `Filtered: window titles containing "invoice" · excluding Firefox`, with a *Clear filter*
  button.
- **Set Status**: records time away from the computer's normal use. Pick *Out to lunch*, *Meeting*, *Coffee break*,
  *Phone call*, *Away from desk*, *Errand* or type another status, then *Start Now*: a record with `trackz-viz` as the
  application (`app_name`), the status as the window title (`window_name`) and `trackz-status` as `process_name` is
  started, and an *I'm back* box stays up until you click it or press Enter, which ends the record. Charts show
  statuses with an icon for their kind (lunch, meeting, coffee, phone, errand, away, or an hourglass for others,
  chosen from words in the status), and `trackz-viz` with the Trackz logo. The tray menu
  also offers *I'm back* meanwhile. The time within a status counts for the status only: reports leave it out of the
  window that had focus meanwhile (usually `trackz-viz` itself) and of screen-lock periods. Quitting `trackz-viz`
  ends the status; if `trackz-viz` was killed, the box comes back at its next start.
- **Custom range**: the *Custom…* button opens a dialog with two calendars and quick picks (this/last week, this/last
  month, this year). Enter applies, Esc or a click outside cancels.
- **Custom Query**: any `SELECT` (or `WITH ... SELECT`), with SQL syntax highlighting (double-click selects a word,
  triple-click a line; keep the button down after either and drag to extend the selection by words or lines), shown as a bar, line, pie or
  time-series chart, or as a table; exportable as CSV. Queries run on a read-only connection. For charts, as in the
  original viewer, name the columns to choose what is plotted: `AS label` for the labels and `AS value`,
  `AS total_time` or `AS duration` for the values. Values may be numbers or `H:MM:SS` text such as
  `time(focus_end_time - focus_start_time, 'unixepoch')`; columns whose name mentions time, duration or seconds are
  shown as durations. For example:
  ```sql
  SELECT process_name, window_name AS label,
         SUM(focus_end_time - focus_start_time) AS duration
  FROM trackz
  WHERE focus_start_time >= unixepoch('now', '-7 days')
  GROUP BY process_name, window_name
  ORDER BY duration DESC
  LIMIT 20
  ```
  `norm_title(window_name)` applies the title rules (see *Merged titles*), so `SELECT norm_title(window_name) AS
  label, ... GROUP BY process_name, label` groups titles the way the dashboard does.
- **Merged titles**: web apps put notification counts in the title (`(3) YouTube`, `Inbox (12) - … - Gmail`,
  `(5) Feed | LinkedIn`). Reports add such titles up as one entry (the tooltip says how many titles were merged);
  the recorded titles are not changed. The rules are regular expressions, applied in order, editable in Settings
  (one per line, `regular expression => replacement`, with a box to try a title) and stored as `title_rules` in
  `config.json`; `merge_titles: false` turns merging off. The defaults remove a count at the start (`(3)`, `(12+)`,
  `[2]`), an unread marker at the start (`*`, `•`), and a count after Inbox, Chat, Messages, Notifications, Unread,
  Feed or Mentions, but leave other numbers alone (`report (1).pdf` stays as it is). The title filter matches titles as
  shown, i.e. after merging.
- **Saved queries**: *Save query* stores the query under a name, as `<name>.sql` in the `saved_queries` directory next
  to the config file (`~/.config/trackz/saved_queries` on Linux/BSD,
  `~/Library/Application Support/trackz/saved_queries` on macOS, `%AppData%\trackz\saved_queries` on Windows). The
  *Saved query* box above the editor lists them; type to filter, then pick one (or press Enter) to load it into the
  editor, where it can be changed before running. The files are plain SQL, so they can also be edited, copied or
  version-controlled outside Trackz.
- **Tray icon**: while `trackz-viz` runs, the Trackz logo sits in the system tray. Clicking it brings the window to
  the front (restoring it if minimised); its context menu has *Show Trackz* and *Quit*. With the tray icon shown,
  closing the window does not quit `trackz-viz`: the window goes away, the program keeps running in the tray, and a
  click on the icon (or *Show Trackz*) opens the window again as you left it, with the same size, maximised or full
  screen if it was. Quit from the tray menu. Where no tray
  icon can be shown, closing the window quits as usual.
- **Single instance**: only one `trackz-viz` runs per user. Starting it again (from the menu, a launcher or a
  terminal) brings the running one to the front, reopening its window if it was closed to the tray, and exits. Linux/BSD use the
  StatusNotifierItem protocol, which KDE, XFCE, LXQt, Cinnamon, MATE, Budgie and waybar support; GNOME needs the
  *AppIndicator and KStatusNotifierItem Support* extension. On Wayland, desktops may refuse to let an application
  focus its own window; the click then only highlights it.
- **Recorder check**: `trackz-viz` checks at start-up and every 30 seconds whether `trackz` is recording to the
  database it shows. If not, it says so in a message box (again whenever `trackz` stops) and in the status bar for as
  long as the problem lasts. At start-up it first gives `trackz` up to 10 seconds to start (the installer, and
  sign-in, often start both together), showing *Waiting for the trackz recorder to start…* meanwhile.
- **Appearance**: follows the desktop's dark/light preference (XDG portal, GNOME, GTK and KDE settings on Linux/BSD;
  the system setting on macOS and Windows) and switches live; it can also be forced to light or dark in Settings.
- **Settings**: appearance, refresh interval, display time zone, date and time formats (strftime-style, e.g. `%d/%m/%Y`), rows (Dashboard and Top Apps) and the
  minimum total for an entry to be listed: windows or applications with less than 120 seconds in the period are hidden
  from the dashboard and the app charts by default. Unlike the original viewer, which dropped every *record* shorter
  than 120 seconds, short visits still count: ten 20-second visits to a window make a 200-second entry. Also in
  Settings: merging titles and its rules (see *Merged titles*).

Settings are stored in `config.json` in the same directory as the default database: `$XDG_CONFIG_HOME/trackz/` or
`~/.config/trackz/` (Linux/BSD), `~/Library/Application Support/trackz/` (macOS), `%APPDATA%\trackz\` (Windows).
`TRACKZ_CONFIG` overrides the file's location.

### Logo and icons

The logo source is [internal/assets/logo.svg](./internal/assets/logo.svg). `make icons` regenerates everything derived
from it: the PNGs embedded in `trackz-viz`, the hicolor icon theme files under `packaging/icons`, `packaging/windows/trackz.ico`
and the Windows resource files `cmd/trackz-viz/rsrc_windows_*.syso`, plus the installer's images and `License.rtf`
(from `LICENSE`).

The window icon is set as follows: on X11 by `trackz-viz` itself (`_NET_WM_ICON`); on Wayland from the installed
`trackz-viz.desktop` and `trackz` icon (`make install` installs both); on Windows from the embedded resources; on macOS
`trackz-viz` sets its Dock icon at start-up.

## Data schema and notes

The schema is unchanged; see the [schema file](./trackz_schema.sql), which includes annotations per field.

`trackz` records have a `focus_start_time` column and a `focus_end_time`. These values are stored as UNIX epoch
timestamps (UTC). Display time zone and formats are applied by the viewer.

**Note that `focus_start_time` is specific to a record/event; it's when the window received focus, not necessarily when the
process was launched. Each process will likely result in multiple records, as you toggle between windows (and tabs).**

- `process_name` is the executable name: `/proc/<pid>/comm` on Linux (as devilspie2 recorded it, at most 15
  characters), the process command name on BSD and macOS, and the executable name without `.exe` on Windows.
- `app_name` is the X11 `WM_CLASS` class (e.g. `firefox-esr`, `XTerm`, `Slack`) on Linux/BSD, the localized
  application name on macOS, and the executable's *FileDescription* (e.g. `Firefox`) on Windows. (devilspie2 used
  libwnck's guessed application name, which for many applications was the title of their first window.)
- While the screen is locked or the machine sleeps, a record with `process_name = 'sleep'`,
  `app_name = 'sleep_mode'`, `window_name = 'System Sleep'` and `process_owner = 'system'` covers the gap. Exclude
  it with `WHERE app_name <> 'sleep_mode'`.
- The record of the focused window is open (`focus_end_time` is `NULL`) until focus moves.

## Migrating from the devilspie2 version

1. Stop and disable the `devilspie2` service and remove the Lua hooks.
2. Back up the database.
3. The Lua hooks stored times shifted by your UTC offset (`os.time(os.date("!*t"))` interprets the UTC wall clock
   as local time), so data recorded during e.g. British Summer Time is an hour early. Correct it **once**:
   ```sh
   $ trackz fix-timestamps -db /etc/trackz/trackz.db -tz Europe/London          # dry run: shows what would change
   $ trackz fix-timestamps -db /etc/trackz/trackz.db -tz Europe/London -apply
   ```
   Do this before `trackz` writes to the database, or pass `-max-id` with the last id written by the Lua hooks.
   Sleep records written by the old Rust viewer already used Unix time and are left alone.
4. Start `trackz` with `TRACKZ_DB` (or `-db`) pointing at the existing database.

## Useful queries

The sample outputs for the below queries were generated using the `sqlite3` CLI client with `.mode box --wrap 30`.
See [4. Changing Output Formats](https://www.sqlite.org/cli.html) for supported output modes. One that may be of
particular interest is `.mode csv`, which you could use to export the data for further manipulation elsewhere.

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

-----

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

You can replace `where process_name='firefox-esr'` with `where window_name like '%project Z%'` to get all events 
generated from windows that pertain to that project (LibreOffice, browser, terminal, etc), because the window title will
likely include that string (be it in the filename, title of the web page or the name of the Slack channel).

------

Aggregate events by `process_name`, `window_name` and day (calculated from `focus_start_time`; add `'localtime'` to `datetime()` to group by local rather than UTC day), 
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


## Architecture

The code follows a model / controller / view split so that more frontends (e.g. a terminal UI) can be added without
touching the logic:

| Package | Role |
|---|---|
| `internal/store` | model: SQLite access (schema, inserts, queries, read-only custom queries, timestamp migration) |
| `internal/tracker` | event logic and the platform backends (libwnck, macOS, Windows, D-Bus) |
| `internal/report` | controller: time ranges, all reports as frontend-neutral `Series`/`Table` values, custom-query charting, CSV export |
| `internal/config`, `internal/timefmt` | settings shared by every binary; time zone and strftime formatting |
| `internal/icons` | application icon lookup per platform |
| `internal/queries` | saved custom queries (`<name>.sql` files) |
| `internal/highlight` | SQL syntax-highlighting spans ([gohighlight](https://github.com/jessp01/gohighlight) with its `sql.yaml`) |
| `internal/systheme` | desktop dark/light preference |
| `internal/tray` | system tray icon and menu (StatusNotifierItem/dbusmenu, Shell_NotifyIcon, NSStatusItem) |
| `ui/gio` | the Gio frontend: draws what `report` returns and passes user input back |
| `cmd/trackz`, `cmd/trackz-viz` | entry points |

A new frontend calls `report.Open`, then `Service.Run(ctx, view, range)` or `Service.CustomQuery`, and renders the
returned `report.Result`.

[license]: ./LICENSE
[badge-license]: https://img.shields.io/github/license/solworktech/trecs.svg
