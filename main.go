package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/godbus/dbus/v5"
)

func main() {
	log.SetFlags(log.Flags() | log.Lshortfile)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func SystemBus(ctx context.Context) (*dbus.Conn, error) {
	conn, err := dbus.SystemBusPrivate(dbus.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if err = conn.Auth(nil); err != nil {
		conn.Close()
		return nil, err
	}
	if err = conn.Hello(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

type State uint32

const (
	StateUnknown     State = 5
	StateCharging    State = 1
	StateDischarging State = 2
)

type BatteryStatus struct {
	Percentage float64
	// 1 charging, 2 discharging, 5 unknown
	State State
}

func notify(title, message string, timeout time.Duration) error {
	appIcon := "battery-full"

	conn, err := dbus.SessionBus()
	if err != nil {
		return err
	}
	obj := conn.Object("org.freedesktop.Notifications", dbus.ObjectPath("/org/freedesktop/Notifications"))

	call := obj.Call("org.freedesktop.Notifications.Notify", 0, "", uint32(0), appIcon, title, message, []string{}, map[string]dbus.Variant{}, int32(timeout.Milliseconds()))
	if call.Err != nil {
		return call.Err
	}
	return nil
}

func getBatteryStatus(ctx context.Context) (BatteryStatus, error) {
	conn, err := SystemBus(ctx)
	if err != nil {
		return BatteryStatus{}, err
	}
	defer conn.Close()

	percentVar, err := conn.Object("org.freedesktop.UPower", "/org/freedesktop/UPower/devices/DisplayDevice").GetProperty("org.freedesktop.UPower.Device.Percentage")
	if err != nil {
		return BatteryStatus{}, err
	}
	percent := percentVar.Value().(float64)

	stateVar, err := conn.Object("org.freedesktop.UPower", "/org/freedesktop/UPower/devices/DisplayDevice").GetProperty("org.freedesktop.UPower.Device.State")
	if err != nil {
		return BatteryStatus{}, err
	}
	state := stateVar.Value().(uint32)

	return BatteryStatus{
		Percentage: percent,
		State:      State(state),
	}, nil
}

type Daemon struct {
	Status BatteryStatus
}

func (d *Daemon) checkPowerLevels(ctx context.Context) error {
	status, err := getBatteryStatus(ctx)
	if err != nil {
		return err
	}
	log.Printf("percentage: %f", status.Percentage)
	log.Printf("state: %v", status.State)

	if d.Status.State != status.State {
		if status.State == StateDischarging {
			if err := notify("Battery Discharging", fmt.Sprintf("%.2f%%", status.Percentage), 1*time.Second); err != nil {
				return err
			}
		} else if status.State == StateCharging {
			if err := notify("Battery Charging", fmt.Sprintf("%.2f%%", status.Percentage), 1*time.Second); err != nil {
				return err
			}
		}
	}

	batteryThreshold := 10.0

	if d.Status.Percentage >= batteryThreshold && status.Percentage < batteryThreshold {
		if err := notify("Battery Low! Charge now!", fmt.Sprintf("%.2f%%", status.Percentage), 1*time.Hour); err != nil {
			return err
		}
	}

	d.Status = status

	return nil
}

func run() error {
	ctx := context.Background()
	// check power levels once a minute
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var d Daemon

	for {
		select {
		case <-ticker.C:
			if err := d.checkPowerLevels(ctx); err != nil {
				log.Printf("failed to check power levels: %v", err)
			}
		}
	}
}
