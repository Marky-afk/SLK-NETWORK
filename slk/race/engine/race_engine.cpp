#include "race_engine.h"
#include <signal.h>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <pthread.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/wait.h>

extern "C" double slk_get_cpu_temp(void);

static double g_distance_left = 0.0;
static int    g_running       = 0;
static int    g_mode          = 1; // 1=FULL, 0=COOL
static int    g_status        = RACE_OK;
static pid_t  g_stress_pid    = -1;

static RacerTelemetry g_telemetry = {0};
static pthread_t      g_telemetry_thread;
static pthread_mutex_t g_mutex = PTHREAD_MUTEX_INITIALIZER;

static double read_cpu_power() {
    const char* path = "/sys/class/powercap/intel-rapl:0/energy_uj";
    unsigned long long e1 = 0, e2 = 0;
    FILE *f = fopen(path, "r");
    if (!f) return 10.0;
    fscanf(f, "%llu", &e1);
    fclose(f);
    usleep(500000);
    f = fopen(path, "r");
    if (!f) return 10.0;
    fscanf(f, "%llu", &e2);
    fclose(f);
    double watts = (double)(e2 - e1) / 500000.0;
    if (watts < 1.0 || watts > 300.0) return 10.0;
    return watts;
}

static void kill_stress() {
    if (g_stress_pid > 0) {
        kill(g_stress_pid, SIGTERM);
        waitpid(g_stress_pid, NULL, 0);
        g_stress_pid = -1;
    }
}

// FULL SPEED: all 4 cores + vm + cache + matrix — maximum electricity draw
static pid_t spawn_full_speed() {
    pid_t pid = fork();
    if (pid == 0) {
        int devnull = open("/dev/null", O_WRONLY);
        dup2(devnull, 1);
        dup2(devnull, 2);
        close(devnull);
        execlp("stress-ng", "stress-ng",
               "--cpu", "4",
               "--cpu-method", "fft",
               "--vm", "2",
               "--vm-bytes", "512M",
               "--cache", "2",
               "--matrix", "2",
               NULL);
        exit(1);
    }
    return pid;
}

// COOL DOWN: 1 core only, light load
static pid_t spawn_cool_down() {
    pid_t pid = fork();
    if (pid == 0) {
        int devnull = open("/dev/null", O_WRONLY);
        dup2(devnull, 1);
        dup2(devnull, 2);
        close(devnull);
        execlp("stress-ng", "stress-ng",
               "--cpu", "1",
               "--cpu-method", "fft",
               NULL);
        exit(1);
    }
    return pid;
}

static void* telemetry_loop(void* arg) {
    while (g_running) {
        double temp  = slk_get_cpu_temp();
        double power = read_cpu_power();

        pthread_mutex_lock(&g_mutex);
        g_telemetry.cpu_temp_celsius = temp;
        g_telemetry.cpu_power_watts  = power;
        g_telemetry.distance_left    = g_distance_left;
        g_telemetry.status           = g_status;

        if (g_running && g_status == RACE_OK) {
            // Normalize speed: baseline 15W laptop = 0.5 m/s at full speed
            // More powerful machine = faster, but all finish in ~2 min for 50m
            // Target: finish 10000m in ~2 minutes (120s)
            // Need ~83 m/s. Power-based bonus on top.
            double base_speed = 80.0;
            double power_bonus = power / 15.0 * 5.0;
            double full_speed_mps = base_speed + power_bonus;
            if (full_speed_mps < 50.0) full_speed_mps = 50.0;
            if (full_speed_mps > 120.0) full_speed_mps = 120.0;
            double speed = g_mode ? full_speed_mps : full_speed_mps * 0.25;
            g_distance_left -= speed;
            g_telemetry.current_speed = speed;
            if (g_distance_left <= 0.0) {
                g_distance_left = 0.0;
                g_telemetry.distance_left = 0.0;
                g_status = RACE_FINISHED;
                g_telemetry.status = RACE_FINISHED;
            }
        }
        pthread_mutex_unlock(&g_mutex);
        usleep(1000000);
    }
    return NULL;
}

int c_start_race(int discipline, double distance) {
    if (g_running) {
        g_running = 0;
        pthread_join(g_telemetry_thread, NULL);
        kill_stress();
    }
    g_distance_left           = distance;
    g_running                 = 1;
    g_mode                    = 1;
    g_status                  = RACE_OK;
    g_telemetry.distance_left = distance;
    g_telemetry.status        = RACE_OK;
    g_telemetry.cpu_power_watts  = 0.0;
    g_telemetry.cpu_temp_celsius = 0.0;
    g_stress_pid = spawn_full_speed();
    pthread_create(&g_telemetry_thread, NULL, telemetry_loop, NULL);
    return RACE_OK;
}

RacerTelemetry c_get_telemetry(void) {
    pthread_mutex_lock(&g_mutex);
    RacerTelemetry t = g_telemetry;
    pthread_mutex_unlock(&g_mutex);
    return t;
}

void c_set_throttle(int full_speed) {
    pthread_mutex_lock(&g_mutex);
    if (full_speed && g_mode == 0) {
        // Switch to full speed
        g_mode = 1;
        kill_stress();
        g_stress_pid = spawn_full_speed();
    } else if (!full_speed && g_mode == 1) {
        // Switch to cool down
        g_mode = 0;
        kill_stress();
        g_stress_pid = spawn_cool_down();
    }
    pthread_mutex_unlock(&g_mutex);
}

void c_stop_race(void) {
    g_running = 0;
    pthread_join(g_telemetry_thread, NULL);
    kill_stress();
}
