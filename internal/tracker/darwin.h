#ifndef TRACKZ_DARWIN_H
#define TRACKZ_DARWIN_H

// Set up notification observers. promptAX asks macOS to show the
// Accessibility permission prompt if the process is not yet trusted.
// Returns 1 if Accessibility access is granted (window titles available).
int trackz_darwin_init(int promptAX);
// Run the main run loop until trackz_darwin_quit() is called.
void trackz_darwin_run(void);
// Thread-safe.
void trackz_darwin_quit(void);

#endif
