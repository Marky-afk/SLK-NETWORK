#ifndef RACE_ENGINE_H
#define RACE_ENGINE_H

#ifdef __cplusplus
extern "C" {
#endif

// Race disciplines
#define DISCIPLINE_SWIMMING  0  // CPU only (stress-ng)
#define DISCIPLINE_CYCLING   1  // GPU only (FurMark)
#define DISCIPLINE_RUNNING   2  // CPU + GPU (both)

// Race status codes
#define RACE_OK              0
#define RACE_ACCIDENT        1
#define RACE_FINISHED        2
#define RACE_ERROR          -1

// Telemetry data from the race engine
typedef struct {
    double cpu_power_watts;
    double cpu_temp_celsius;
    double gpu_power_watts;
    double gpu_temp_celsius;
    double distance_left;
    double current_speed;
    int    status;
    int    step_attempts;
} RacerTelemetry;

// Start a race
int  c_start_race(int discipline, double distance);

// Get current telemetry
RacerTelemetry c_get_telemetry(void);

// Set throttle (1=full speed, 0=slow down)
void c_set_throttle(int full_speed);

// Stop the race
void c_stop_race(void);

#ifdef __cplusplus
}
#endif

#endif
