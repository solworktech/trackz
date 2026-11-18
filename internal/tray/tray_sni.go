//go:build linux || freebsd || openbsd || netbsd || dragonfly

package tray

import (
	"fmt"
	"image"
	"image/draw"
	"log"
	"os"
	"sync"
	"sync/atomic"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// StatusNotifierItem (https://www.freedesktop.org/wiki/Specifications/StatusNotifierItem/)
// with its menu exported through com.canonical.dbusmenu.

const (
	sniIface     = "org.kde.StatusNotifierItem"
	sniPath      = dbus.ObjectPath("/StatusNotifierItem")
	menuIface    = "com.canonical.dbusmenu"
	menuPath     = dbus.ObjectPath("/MenuBar")
	watcherName  = "org.kde.StatusNotifierWatcher"
	watcherPath  = dbus.ObjectPath("/StatusNotifierWatcher")
	watcherIface = "org.kde.StatusNotifierWatcher"
)

type sniTray struct {
	opts     Options
	conn     *dbus.Conn
	busName  string
	props    *prop.Properties
	mu       sync.Mutex
	items    []Item
	revision atomic.Uint32
	shown    atomic.Bool // registered with a StatusNotifierWatcher
	done     chan struct{}
	once     sync.Once
}

// pixmap is the (iiay) icon structure: width, height, ARGB32 in network
// byte order.
type pixmap struct {
	W, H int32
	Data []byte
}

type tooltip struct {
	IconName string
	Icons    []pixmap
	Title    string
	Text     string
}

func newImpl(opts Options) (impl, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("tray: no D-Bus session bus: %w", err)
	}
	t := &sniTray{
		opts:    opts,
		conn:    conn,
		busName: fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid()),
		items:   opts.Items,
		done:    make(chan struct{}),
	}
	t.revision.Store(1)
	if err := t.export(); err != nil {
		_ = conn.Close() // the export error is the one to report
		return nil, err
	}
	if reply, err := conn.RequestName(t.busName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		_ = conn.Close()
		return nil, fmt.Errorf("tray: cannot own %s: %v", t.busName, err)
	}
	go t.watch()
	return t, nil
}

func pixmaps(icon func(int) image.Image) []pixmap {
	if icon == nil {
		return nil
	}
	var out []pixmap
	for _, size := range []int{16, 22, 24, 32, 48, 64} {
		src := icon(size)
		if src == nil {
			continue
		}
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)
		data := make([]byte, 0, size*size*4)
		for i := 0; i < len(img.Pix); i += 4 {
			// NRGBA -> ARGB, network byte order.
			data = append(data, img.Pix[i+3], img.Pix[i], img.Pix[i+1], img.Pix[i+2])
		}
		out = append(out, pixmap{W: int32(size), H: int32(size), Data: data})
	}
	return out
}

func (t *sniTray) export() error {
	icons := pixmaps(t.opts.Icon)
	ro := func(v any) *prop.Prop { return &prop.Prop{Value: v, Writable: false, Emit: prop.EmitTrue} }
	props, err := prop.Export(t.conn, sniPath, prop.Map{
		sniIface: {
			"Category":            ro("ApplicationStatus"),
			"Id":                  ro(t.opts.ID),
			"Title":               ro(t.opts.Title),
			"Status":              ro("Active"),
			"WindowId":            ro(int32(0)),
			"IconName":            ro(""),
			"IconThemePath":       ro(""),
			"IconPixmap":          ro(icons),
			"OverlayIconName":     ro(""),
			"OverlayIconPixmap":   ro([]pixmap{}),
			"AttentionIconName":   ro(""),
			"AttentionIconPixmap": ro([]pixmap{}),
			"AttentionMovieName":  ro(""),
			"ToolTip":             ro(tooltip{Title: t.opts.Title, Text: t.opts.Tooltip, Icons: []pixmap{}}),
			// false: a left click calls Activate; the menu is for the
			// right button.
			"ItemIsMenu": ro(false),
			"Menu":       ro(menuPath),
		},
	})
	if err != nil {
		return fmt.Errorf("tray: export properties: %w", err)
	}
	t.props = props
	if err := t.conn.Export(sniMethods{t}, sniPath, sniIface); err != nil {
		return err
	}
	menuProps, err := prop.Export(t.conn, menuPath, prop.Map{
		menuIface: {
			"Version":       ro(uint32(3)),
			"TextDirection": ro("ltr"),
			"Status":        ro("normal"),
			"IconThemePath": ro([]string{}),
		},
	})
	if err != nil {
		return fmt.Errorf("tray: export menu properties: %w", err)
	}
	if err := t.conn.Export(menuMethods{t}, menuPath, menuIface); err != nil {
		return err
	}
	sniNode := &introspect.Node{
		Name: string(sniPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name:       sniIface,
				Methods:    introspect.Methods(sniMethods{t}),
				Properties: props.Introspection(sniIface),
				Signals: []introspect.Signal{
					{Name: "NewTitle"}, {Name: "NewIcon"}, {Name: "NewAttentionIcon"},
					{Name: "NewOverlayIcon"}, {Name: "NewToolTip"},
					{Name: "NewStatus", Args: []introspect.Arg{{Name: "status", Type: "s"}}},
				},
			},
		},
	}
	menuNode := &introspect.Node{
		Name: string(menuPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name:       menuIface,
				Methods:    introspect.Methods(menuMethods{t}),
				Properties: menuProps.Introspection(menuIface),
				Signals: []introspect.Signal{
					{Name: "ItemsPropertiesUpdated", Args: []introspect.Arg{
						{Name: "updatedProps", Type: "a(ia{sv})"}, {Name: "removedProps", Type: "a(ias)"}}},
					{Name: "LayoutUpdated", Args: []introspect.Arg{
						{Name: "revision", Type: "u"}, {Name: "parent", Type: "i"}}},
				},
			},
		},
	}
	if err := t.conn.Export(introspect.NewIntrospectable(sniNode), sniPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	return t.conn.Export(introspect.NewIntrospectable(menuNode), menuPath, "org.freedesktop.DBus.Introspectable")
}

// watch registers with the StatusNotifierWatcher now and whenever one
// (re)appears, e.g. after the panel restarts.
func (t *sniTray) watch() {
	sigs := make(chan *dbus.Signal, 8)
	t.conn.Signal(sigs)
	if err := t.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, watcherName),
	); err != nil {
		// Still register now; only re-registering after a panel restart
		// is lost.
		log.Printf("tray: cannot watch for the tray host restarting: %v", err)
	}
	t.register()
	for {
		select {
		case <-t.done:
			return
		case s, ok := <-sigs:
			if !ok {
				return
			}
			if s.Name == "org.freedesktop.DBus.NameOwnerChanged" && len(s.Body) == 3 {
				if newOwner, _ := s.Body[2].(string); newOwner != "" {
					t.register()
				} else {
					t.shown.Store(false) // the panel went away
				}
			}
		}
	}
}

func (t *sniTray) register() {
	call := t.conn.Object(watcherName, watcherPath).Call(watcherIface+".RegisterStatusNotifierItem", 0, t.busName)
	t.shown.Store(call.Err == nil)
}

func (t *sniTray) active() bool { return t.shown.Load() }

func (t *sniTray) setItems(items []Item) error {
	t.mu.Lock()
	t.items = items
	t.mu.Unlock()
	rev := t.revision.Add(1)
	return t.conn.Emit(menuPath, menuIface+".LayoutUpdated", rev, int32(0))
}

func (t *sniTray) close() {
	t.once.Do(func() {
		close(t.done)
		// Closing the connection drops the name and the icon anyway; there
		// is nothing to do about a failure while shutting down.
		_, _ = t.conn.ReleaseName(t.busName)
		_ = t.conn.Close()
	})
}

// sniMethods are the org.kde.StatusNotifierItem methods.
type sniMethods struct{ t *sniTray }

func (m sniMethods) Activate(x, y int32) *dbus.Error {
	run(m.t.opts.OnActivate)
	return nil
}

// SecondaryActivate is usually a middle click; treat it like a click.
func (m sniMethods) SecondaryActivate(x, y int32) *dbus.Error {
	run(m.t.opts.OnActivate)
	return nil
}

// ContextMenu is only called by hosts that do not use the Menu property.
func (m sniMethods) ContextMenu(x, y int32) *dbus.Error                 { return nil }
func (m sniMethods) Scroll(delta int32, orientation string) *dbus.Error { return nil }

// menuLayout is the (ia{sv}av) dbusmenu layout node.
type menuLayout struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

type menuItemProps struct {
	ID    int32
	Props map[string]dbus.Variant
}

// Menu item ids are index+1; 0 is the root.
func (t *sniTray) itemProps(i int, it Item) map[string]dbus.Variant {
	if it.Label == "" {
		return map[string]dbus.Variant{"type": dbus.MakeVariant("separator")}
	}
	return map[string]dbus.Variant{
		"label":   dbus.MakeVariant(it.Label),
		"enabled": dbus.MakeVariant(true),
		"visible": dbus.MakeVariant(true),
	}
}

func (t *sniTray) snapshot() []Item {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Item(nil), t.items...)
}

// menuMethods are the com.canonical.dbusmenu methods.
type menuMethods struct{ t *sniTray }

func (m menuMethods) GetLayout(parentID int32, depth int32, names []string) (uint32, menuLayout, *dbus.Error) {
	items := m.t.snapshot()
	if parentID != 0 {
		if parentID < 0 || int(parentID) > len(items) {
			return 0, menuLayout{}, dbus.MakeFailedError(fmt.Errorf("no menu item %d", parentID))
		}
		return m.t.revision.Load(), menuLayout{ID: parentID, Props: m.t.itemProps(int(parentID)-1, items[parentID-1]), Children: []dbus.Variant{}}, nil
	}
	root := menuLayout{ID: 0, Props: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}, Children: []dbus.Variant{}}
	if depth != 0 {
		for i, it := range items {
			root.Children = append(root.Children, dbus.MakeVariant(menuLayout{
				ID: int32(i + 1), Props: m.t.itemProps(i, it), Children: []dbus.Variant{},
			}))
		}
	}
	return m.t.revision.Load(), root, nil
}

func (m menuMethods) GetGroupProperties(ids []int32, names []string) ([]menuItemProps, *dbus.Error) {
	items := m.t.snapshot()
	var out []menuItemProps
	for _, id := range ids {
		if id >= 1 && int(id) <= len(items) {
			out = append(out, menuItemProps{ID: id, Props: m.t.itemProps(int(id)-1, items[id-1])})
		}
	}
	return out, nil
}

func (m menuMethods) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	items := m.t.snapshot()
	if id >= 1 && int(id) <= len(items) {
		if v, ok := m.t.itemProps(int(id)-1, items[id-1])[name]; ok {
			return v, nil
		}
	}
	return dbus.MakeVariant(""), nil
}

func (m menuMethods) Event(id int32, eventID string, data dbus.Variant, timestamp uint32) *dbus.Error {
	m.event(id, eventID)
	return nil
}

// event handles a menu event and reports whether item id exists.
func (m menuMethods) event(id int32, eventID string) bool {
	items := m.t.snapshot()
	if id < 1 || int(id) > len(items) {
		return false
	}
	if eventID == "clicked" {
		run(items[id-1].OnClick)
	}
	return true
}

type menuEvent struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

// EventGroup returns the ids it found no item for, as the protocol asks.
func (m menuMethods) EventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	missing := []int32{}
	for _, e := range events {
		if !m.event(e.ID, e.EventID) {
			missing = append(missing, e.ID)
		}
	}
	return missing, nil
}

func (m menuMethods) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }

func (m menuMethods) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return []int32{}, []int32{}, nil
}
