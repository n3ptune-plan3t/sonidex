package mini

// FromKeysym maps an X11/xkb keysym to a KeyEvent with Key or Text set
// (modifier flags are left for the caller). Both the X11 and Wayland
// backends speak keysyms, so the table lives here.
func FromKeysym(ks uint32) KeyEvent {
	var ev KeyEvent
	switch ks {
	case 0xff08:
		ev.Key = KeyBackspace
	case 0xffff:
		ev.Key = KeyDelete
	case 0xff51:
		ev.Key = KeyLeft
	case 0xff53:
		ev.Key = KeyRight
	case 0xff52:
		ev.Key = KeyUp
	case 0xff54:
		ev.Key = KeyDown
	case 0xff50:
		ev.Key = KeyHome
	case 0xff57:
		ev.Key = KeyEnd
	case 0xff0d, 0xff8d: // Return, KP_Enter
		ev.Key = KeyEnter
	case 0xff1b:
		ev.Key = KeyEscape
	case 0xff09:
		ev.Key = KeyTab
	case 0xfe20: // ISO_Left_Tab (Shift+Tab)
		ev.Key, ev.Shift = KeyTab, true
	case 0xffae: // KP_Decimal
		ev.Text = "."
	default:
		switch {
		case ks >= 0x20 && ks <= 0x7e, ks >= 0xa0 && ks <= 0xff:
			ev.Text = string(rune(ks))
		case ks&0xff000000 == 0x01000000: // Unicode keysym
			ev.Text = string(rune(ks & 0xffffff))
		case ks >= 0xffb0 && ks <= 0xffb9: // keypad digits
			ev.Text = string(rune('0' + (ks - 0xffb0)))
		}
	}
	return ev
}
