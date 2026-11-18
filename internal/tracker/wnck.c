//go:build (linux || freebsd || openbsd || netbsd || dragonfly) && cgo && !nownck

#define WNCK_I_KNOW_THIS_IS_UNSTABLE 1
#include <libwnck/libwnck.h>
#include <gdk/gdk.h>
#include "wnck.h"
#include "_cgo_export.h"

static GMainLoop *loop = NULL;
#if WNCK_CHECK_VERSION(43, 0, 0)
static WnckHandle *handle = NULL;
#endif
static WnckScreen *screen = NULL;
static WnckWindow *tracked = NULL;
static gulong name_handler = 0;

static void report(WnckWindow *w) {
	if (w == NULL)
		return;
	WnckApplication *app = wnck_window_get_application(w);
	int pid = wnck_window_get_pid(w);
	if (pid == 0 && app != NULL)
		pid = wnck_application_get_pid(app);
	const char *title = wnck_window_get_name(w);
	const char *app_name = app != NULL ? wnck_application_get_name(app) : NULL;
	const char *instance = wnck_window_get_class_instance_name(w);
	const char *klass = wnck_window_get_class_group_name(w);
	goWnckWindow(pid, (char *)(title ? title : ""), (char *)(app_name ? app_name : ""),
	             (char *)(instance ? instance : ""), (char *)(klass ? klass : ""));
}

static void on_name_changed(WnckWindow *w, gpointer data) {
	(void)data;
	// Only the focused window's title matters; devilspie2 fired for any
	// window, which recorded background windows as if they were focused.
	if (w == tracked)
		report(w);
}

static void untrack(void) {
	if (tracked != NULL && name_handler != 0)
		g_signal_handler_disconnect(tracked, name_handler);
	tracked = NULL;
	name_handler = 0;
}

static void on_active_changed(WnckScreen *s, WnckWindow *previous, gpointer data) {
	(void)previous;
	(void)data;
	WnckWindow *w = wnck_screen_get_active_window(s);
	if (w == tracked)
		return;
	untrack();
	if (w == NULL)
		return;
	tracked = w;
	name_handler = g_signal_connect(w, "name-changed", G_CALLBACK(on_name_changed), NULL);
	report(w);
}

static void on_window_closed(WnckScreen *s, WnckWindow *w, gpointer data) {
	(void)s;
	(void)data;
	// The window object is about to be destroyed; drop our handler first.
	if (w == tracked)
		untrack();
}

int trackz_wnck_init(void) {
	// libwnck only works with X11 (or XWayland).
	gdk_set_allowed_backends("x11");
	if (!gdk_init_check(NULL, NULL))
		return -1;
#if WNCK_CHECK_VERSION(43, 0, 0)
	handle = wnck_handle_new(WNCK_CLIENT_TYPE_APPLICATION);
	screen = wnck_handle_get_default_screen(handle);
#else
	screen = wnck_screen_get_default();
#endif
	if (screen == NULL)
		return -2;
	wnck_screen_force_update(screen);
	g_signal_connect(screen, "active-window-changed", G_CALLBACK(on_active_changed), NULL);
	g_signal_connect(screen, "window-closed", G_CALLBACK(on_window_closed), NULL);
	loop = g_main_loop_new(NULL, FALSE);
	return 0;
}

void trackz_wnck_run(void) {
	on_active_changed(screen, NULL, NULL);
	g_main_loop_run(loop);
	untrack();
	g_main_loop_unref(loop);
	loop = NULL;
#if WNCK_CHECK_VERSION(43, 0, 0)
	g_object_unref(handle);
	handle = NULL;
#else
	wnck_shutdown();
#endif
}

static gboolean quit_cb(gpointer data) {
	(void)data;
	if (loop != NULL)
		g_main_loop_quit(loop);
	return G_SOURCE_REMOVE;
}

void trackz_wnck_quit(void) {
	g_idle_add(quit_cb, NULL);
}
