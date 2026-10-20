//go:build windows && amd64

package windowsrunner

import (
	"context"
	"errors"
	"time"
	"unsafe"
)

type systemTime struct {
	Year, Month, DayOfWeek, Day, Hour, Minute, Second, Millisecond uint16
}

func (o *windowsOps) syncClock(ctx context.Context, target time.Time) (int64, int64, error) {
	// Only the fixed SCM guest entry reaches this method. Unit tests inject a
	// setter into updateGuestClock; they never alter the PC's clock.
	if !IsLocalSystem() {
		return 0, 0, errors.New("guest UTC requires LocalSystem")
	}
	return updateGuestClock(ctx, target, time.Now, func(at time.Time) error {
		st := systemTime{Year: uint16(at.Year()), Month: uint16(at.Month()), Day: uint16(at.Day()), Hour: uint16(at.Hour()), Minute: uint16(at.Minute()), Second: uint16(at.Second()), Millisecond: uint16(at.Nanosecond() / 1e6)}
		// SetSystemTime itself enables/restores SE_SYSTEMTIME_NAME for this call.
		ok, _, _ := kernel.NewProc("SetSystemTime").Call(uintptr(unsafe.Pointer(&st)))
		if ok == 0 {
			return errors.New("guest UTC system call failed")
		}
		return nil
	})
}
