package manager

/*
#cgo CFLAGS: -I${SRCDIR}/../../race/engine
#cgo LDFLAGS: -L${SRCDIR}/../../build/linux -lslkrace -lstdc++ -lpthread -lm

#include "race_engine.h"
*/
import "C"
import (
	"fmt"
	"time"
)

type RacerState struct {
	CPUPowerWatts  float64
	CPUTempCelsius float64
	GPUPowerWatts  float64
	GPUTempCelsius float64
	DistanceLeft   float64
	CurrentSpeed   float64
	Status         int
	StepAttempts   int
}

const (
	StatusOK       = 0
	StatusAccident = 1
	StatusFinished = 2
	StatusError    = -1
)

func StartRace(discipline int, distance float64) error {
	result := C.c_start_race(C.int(discipline), C.double(distance))
	if result != 0 {
		return fmt.Errorf("failed to start race: error code %d", result)
	}
	// Discipline: %d | Distance: %.2f meters\n", discipline, distance)
	return nil
}

func GetTelemetry() RacerState {
	t := C.c_get_telemetry()
	return RacerState{
		CPUPowerWatts:  float64(t.cpu_power_watts),
		CPUTempCelsius: float64(t.cpu_temp_celsius),
		GPUPowerWatts:  float64(t.gpu_power_watts),
		GPUTempCelsius: float64(t.gpu_temp_celsius),
		DistanceLeft:   float64(t.distance_left),
		CurrentSpeed:   float64(t.current_speed),
		Status:         int(t.status),
		StepAttempts:   int(t.step_attempts),
	}
}

func SetThrottle(fullSpeed bool) {
	speed := C.int(0)
	if fullSpeed {
		speed = C.int(1)
	}
	C.c_set_throttle(speed)
}

func StopRace() {
	C.c_stop_race()
	//fmt.Println("🛑 Race stopped!")
}

func RunRaceDashboard(distance float64) {
	err := StartRace(0, distance)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("\n====== SLK LIVE RACE DASHBOARD ======")
	fmt.Println("Press Ctrl+C to stop\n")

	for {
		state := GetTelemetry()

		fmt.Printf("\r🏃 DIST LEFT: %8.3f m | CPU: %5.1fW | TEMP: %4.1f°C | SPEED: %.4f m/s | STATUS: %s        ",
			state.DistanceLeft,
			state.CPUPowerWatts,
			state.CPUTempCelsius,
			state.CurrentSpeed,
			statusName(state.Status),
		)

		if state.Status == StatusFinished {
			fmt.Println("\n\n🏆 RACE FINISHED!")
			break
		}

		time.Sleep(1 * time.Second)
	}

	StopRace()
}

func statusName(status int) string {
	switch status {
	case StatusOK:
		return "RACING 🟢"
	case StatusAccident:
		return "ACCIDENT 🔴"
	case StatusFinished:
		return "FINISHED 🏆"
	default:
		return "UNKNOWN"
	}
}
