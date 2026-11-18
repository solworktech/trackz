#ifndef TRACKZ_WNCK_H
#define TRACKZ_WNCK_H

// Initialise GDK (X11 backend) and libwnck. Returns 0 on success.
int trackz_wnck_init(void);
// Run the GLib main loop until trackz_wnck_quit() is called.
void trackz_wnck_run(void);
// Thread-safe: ask the main loop to stop.
void trackz_wnck_quit(void);

#endif
