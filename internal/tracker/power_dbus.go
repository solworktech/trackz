//go:build (linux || freebsd || openbsd || netbsd || dragonfly) && cgo && !nownck

package tracker

import (
	"context"
	"errors"
	"strings"

	"github.com/godbus/dbus/v5"
)

// watchDBusPower reports screen lock (session bus) and system suspend
// (logind, system bus) as power events. It returns when ctx is cancelled or
// when neither bus is available.
//
// Lock: the ActiveChanged(bool) signal of org.freedesktop.ScreenSaver and of
// desktop-specific equivalents (org.gnome.ScreenSaver, org.mate.ScreenSaver,
// org.cinnamon.ScreenSaver, ...), which is what the Rust viewer listened to.
// Suspend: org.freedesktop.login1.Manager.PrepareForSleep(bool).
func watchDBusPower(ctx context.Context, sink Sink, opts BackendOptions) error {
	signals := make(chan *dbus.Signal, 32)
	var conns []*dbus.Conn
	defer func() {
		for _, c := range conns {
			_ = c.Close() // shutting down; nothing more to record
		}
	}()

	if session, err := dbus.ConnectSessionBus(); err != nil {
		opts.debugf("session bus unavailable, screen lock not tracked: %v", err)
	} else {
		conns = append(conns, session)
		if opts.DBusMonitor {
			if err := monitorSession(ctx, session, signals); err != nil {
				opts.debugf("BecomeMonitor failed: %v", err)
			}
		} else if err := session.AddMatchSignal(dbus.WithMatchMember("ActiveChanged")); err != nil {
			opts.debugf("subscribe to ActiveChanged: %v", err)
		} else {
			session.Signal(signals)
		}
	}

	if system, err := dbus.ConnectSystemBus(); err != nil {
		opts.debugf("system bus unavailable, suspend not tracked: %v", err)
	} else {
		conns = append(conns, system)
		if err := system.AddMatchSignal(
			dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
			dbus.WithMatchMember("PrepareForSleep"),
		); err != nil {
			opts.debugf("subscribe to PrepareForSleep: %v", err)
		} else {
			system.Signal(signals)
		}
	}
	if len(conns) == 0 {
		return errors.New("no D-Bus connection: sleep/lock tracking disabled")
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-signals:
			if !ok {
				return nil
			}
			handleSignal(sig.Name, sig.Body, sink, opts)
		}
	}
}

func handleSignal(name string, body []any, sink Sink, opts BackendOptions) {
	if len(body) != 1 {
		return
	}
	active, ok := body[0].(bool)
	if !ok {
		return
	}
	switch {
	case strings.HasSuffix(name, "ScreenSaver.ActiveChanged"):
		opts.debugf("D-Bus %s(%v)", name, active)
		sink.Power(PowerEvent{Source: "lock", Away: active})
	case name == "org.freedesktop.login1.Manager.PrepareForSleep":
		opts.debugf("D-Bus %s(%v)", name, active)
		sink.Power(PowerEvent{Source: "sleep", Away: active})
	}
}

// monitorSession turns conn into a monitor connection and forwards matching
// messages as signals. A monitor connection can do nothing else.
func monitorSession(ctx context.Context, conn *dbus.Conn, out chan<- *dbus.Signal) error {
	rules := []string{"type='signal',member='ActiveChanged'"}
	call := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.Monitoring.BecomeMonitor", 0, rules, uint32(0))
	if call.Err != nil {
		return call.Err
	}
	msgs := make(chan *dbus.Message, 32)
	conn.Eavesdrop(msgs)
	go func() {
		for msg := range msgs {
			if msg.Type != dbus.TypeSignal {
				continue
			}
			iface, _ := msg.Headers[dbus.FieldInterface].Value().(string)
			member, _ := msg.Headers[dbus.FieldMember].Value().(string)
			select {
			case out <- &dbus.Signal{Name: iface + "." + member, Body: msg.Body}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}
