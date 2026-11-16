//go:build linux

package mpris

import (
	"sync"

	"github.com/godbus/dbus/v5"
)

const propertiesIface = "org.freedesktop.DBus.Properties"

// Standard D-Bus property errors, same names godbus's prop package returns so
// clients see no difference.
var (
	errIfaceNotFound = dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", nil)
	errPropNotFound  = dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", nil)
	errReadOnly      = dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", nil)
	errInvalidArg    = dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", nil)
)

// propSpec is one exported property. onSet runs before a client write is
// stored, and only writable properties have one.
type propSpec struct {
	value    dbus.Variant
	writable bool
	onSet    func(dbus.Variant) *dbus.Error
}

// properties serves org.freedesktop.DBus.Properties for the MPRIS object.
//
// It exists because godbus's prop.Export cannot be used safely for an a{sv}
// property. prop allocates storage once per property and merges every update
// into that same allocation with dbus.Store, which for a map destination is an
// in-place storeMapIntoMap rather than a replacement. It then hands that live
// allocation to the encoder twice over: from emitChange, and less obviously
// from Get and GetAll, which return reflect.ValueOf(prop.Value).Elem().
// Interface() and release the lock before the reply is encoded in the
// connection's output goroutine. The Metadata map is therefore read by an
// unlocked encoder while the next track change writes it, which is #110.
//
// The invariant here is that no map is ever written after it escapes. Metadata
// is replaced wholesale on each update instead of merged, and GetAll builds a
// fresh outer map per call, so everything handed to godbus is immutable from
// that moment on. PropertiesChanged is emitted by flush rather than per
// property, which also collapses the 6 to 8 signals a single track change used
// to produce into one.
type properties struct {
	conn *dbus.Conn
	path dbus.ObjectPath

	setMu sync.Mutex // serializes client Set + onSet + emitChanged ordering
	mu    sync.RWMutex
	m     map[string]map[string]*propSpec
}

// newProperties exports org.freedesktop.DBus.Properties for path and returns
// the store backing it.
func newProperties(conn *dbus.Conn, path dbus.ObjectPath, spec map[string]map[string]*propSpec) (*properties, error) {
	p := &properties{conn: conn, path: path, m: spec}
	if err := conn.ExportMethodTable(map[string]any{
		"Get":    p.Get,
		"GetAll": p.GetAll,
		"Set":    p.Set,
	}, path, propertiesIface); err != nil {
		return nil, err
	}
	return p, nil
}

// Get implements org.freedesktop.DBus.Properties.Get.
func (p *properties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	props, ok := p.m[iface]
	if !ok {
		return dbus.Variant{}, errIfaceNotFound
	}
	spec, ok := props[name]
	if !ok {
		return dbus.Variant{}, errPropNotFound
	}
	return spec.value, nil
}

// GetAll implements org.freedesktop.DBus.Properties.GetAll. The returned map
// is built per call, so the caller never shares storage with this store.
func (p *properties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	props, ok := p.m[iface]
	if !ok {
		return nil, errIfaceNotFound
	}
	out := make(map[string]dbus.Variant, len(props))
	for name, spec := range props {
		out[name] = spec.value
	}
	return out, nil
}

// Set implements org.freedesktop.DBus.Properties.Set. Only LoopStatus and
// Shuffle are writable; both route to the player before the value is stored,
// so a rejected control action does not leave the property lying about state.
func (p *properties) Set(iface, name string, v dbus.Variant) *dbus.Error {
	p.setMu.Lock()
	defer p.setMu.Unlock()

	p.mu.Lock()
	props, ok := p.m[iface]
	if !ok {
		p.mu.Unlock()
		return errIfaceNotFound
	}
	spec, ok := props[name]
	if !ok {
		p.mu.Unlock()
		return errPropNotFound
	}
	if !spec.writable {
		p.mu.Unlock()
		return errReadOnly
	}
	if v.Signature() != spec.value.Signature() {
		p.mu.Unlock()
		return errInvalidArg
	}
	onSet := spec.onSet
	p.mu.Unlock()

	// Outside p.mu: the callback calls into the player, while setMu keeps
	// consecutive Set mutations and PropertiesChanged signals in order.
	if onSet != nil {
		if err := onSet(v); err != nil {
			return err
		}
	}

	p.mu.Lock()
	spec.value = v
	p.mu.Unlock()
	_ = p.emitChanged(iface, map[string]dbus.Variant{name: v})
	return nil
}

// get reads a property in-process.
func (p *properties) get(iface, name string) any {
	p.mu.RLock()
	defer p.mu.RUnlock()
	props, ok := p.m[iface]
	if !ok {
		return nil
	}
	spec, ok := props[name]
	if !ok {
		return nil
	}
	return spec.value.Value()
}

// store writes a property in-process without emitting anything. Callers batch
// their changes and emit once via emitChanged. Values must never be mutated
// after being passed here.
func (p *properties) store(iface, name string, value any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if props, ok := p.m[iface]; ok {
		if spec, ok := props[name]; ok {
			spec.value = dbus.MakeVariant(value)
		}
	}
}

// emitChanged sends one PropertiesChanged carrying every property that moved.
// changed must not be touched after this returns.
func (p *properties) emitChanged(iface string, changed map[string]dbus.Variant) error {
	if len(changed) == 0 {
		return nil
	}
	if p.conn == nil {
		return dbus.ErrClosed
	}
	return p.conn.Emit(p.path, propertiesIface+".PropertiesChanged", iface, changed, []string{})
}
