//go:build linux || freebsd || openbsd || netbsd || dragonfly

package tray

import (
	"image"
	"image/color"
	"os"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type fakeWatcher struct{ registered chan string }

func (w fakeWatcher) RegisterStatusNotifierItem(service string) *dbus.Error {
	w.registered <- service
	return nil
}

func solid(size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+3] = 0xff, 0xff
	}
	return img
}

// TestSNI talks to the tray icon the way a panel does.
func TestSNI(t *testing.T) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("no D-Bus session bus")
	}
	activated, quit := make(chan bool, 1), make(chan bool, 1)
	tr, err := New(Options{
		ID: "trackz-test", Title: "Trackz", Tooltip: "test",
		Icon:       func(s int) image.Image { return solid(s) },
		OnActivate: func() { activated <- true },
		Items: []Item{
			{Label: "Show Trackz", OnClick: func() {}},
			{},
			{Label: "Quit", OnClick: func() { quit <- true }},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	if tr.Active() {
		t.Error("no watcher yet: the icon must not count as shown")
	}
	// The panel starts after the application: the item must register when
	// the watcher appears.
	wconn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wconn.Close(); err != nil {
			t.Error(err)
		}
	}()
	w := fakeWatcher{registered: make(chan string, 1)}
	if err := wconn.Export(w, watcherPath, watcherIface); err != nil {
		t.Fatal(err)
	}
	if reply, err := wconn.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own watcher name: %v %v", reply, err)
	}
	var service string
	select {
	case service = <-w.registered:
	case <-time.After(3 * time.Second):
		t.Fatal("item did not register with the watcher")
	}

	deadline := time.Now().Add(2 * time.Second)
	for !tr.Active() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !tr.Active() {
		t.Error("registered icon should be active")
	}

	item := wconn.Object(service, sniPath)
	var props map[string]dbus.Variant
	if err := item.Call("org.freedesktop.DBus.Properties.GetAll", 0, sniIface).Store(&props); err != nil {
		t.Fatal(err)
	}
	if props["Id"].Value() != "trackz-test" || props["ItemIsMenu"].Value() != false || props["Menu"].Value() != menuPath {
		t.Errorf("properties = %v", props)
	}
	var icons []pixmap
	if err := props["IconPixmap"].Store(&icons); err != nil || len(icons) == 0 || icons[0].W != 16 ||
		len(icons[0].Data) != 16*16*4 || icons[0].Data[0] != 0xff || icons[0].Data[1] != 0xff {
		t.Errorf("icon pixmaps = %d (%v)", len(icons), err)
	}

	// Left click.
	if err := item.Call(sniIface+".Activate", 0, int32(0), int32(0)).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case <-activated:
	case <-time.After(2 * time.Second):
		t.Error("Activate did not call OnActivate")
	}

	// Context menu: layout, then click "Quit" (id 3).
	menu := wconn.Object(service, menuPath)
	var rev uint32
	var layout menuLayout
	if err := menu.Call(menuIface+".GetLayout", 0, int32(0), int32(-1), []string{}).Store(&rev, &layout); err != nil {
		t.Fatal(err)
	}
	if len(layout.Children) != 3 {
		t.Fatalf("menu has %d entries", len(layout.Children))
	}
	var third menuLayout
	if err := dbus.Store([]any{layout.Children[2].Value()}, &third); err != nil {
		t.Fatal(err)
	}
	if third.ID != 3 || third.Props["label"].Value() != "Quit" {
		t.Errorf("third entry = %+v", third)
	}
	var second menuLayout
	if err := dbus.Store([]any{layout.Children[1].Value()}, &second); err != nil {
		t.Fatal(err)
	}
	if second.Props["type"].Value() != "separator" {
		t.Errorf("second entry should be a separator: %+v", second)
	}
	if err := menu.Call(menuIface+".Event", 0, int32(3), "clicked", dbus.MakeVariant(""), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case <-quit:
	case <-time.After(2 * time.Second):
		t.Error("clicking Quit did not run its callback")
	}
	_ = color.White
}
