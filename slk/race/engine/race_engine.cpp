#include "race_engine.h"
#include <signal.h>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <pthread.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/wait.h>
#include <math.h>

extern "C" double slk_get_cpu_temp(void);

static double g_distance_left = 0.0;
static int    g_running       = 0;
static int    g_mode          = 1;
static int    g_status        = RACE_OK;
static pid_t  g_stress_pid    = -1;

static RacerTelemetry g_telemetry = {0};
static pthread_t      g_telemetry_thread;
static pthread_mutex_t g_mutex = PTHREAD_MUTEX_INITIALIZER;

static double read_cpu_power() {
    const char* paths[] = {
        "/sys/class/powercap/intel-rapl:0/energy_uj",
        "/sys/class/powercap/intel-rapl/intel-rapl:0/energy_uj",
        NULL
    };
    for (int i = 0; paths[i] != NULL; i++) {
        unsigned long long e1 = 0, e2 = 0;
        FILE *f = fopen(paths[i], "r");
        if (!f) continue;
        fscanf(f, "%llu", &e1);
        fclose(f);
        usleep(500000);
        f = fopen(paths[i], "r");
        if (!f) continue;
        fscanf(f, "%llu", &e2);
        fclose(f);
        double watts = (double)(e2 - e1) / 500000.0;
        if (watts >= 1.0 && watts <= 500.0) return watts;
    }
    return 12.0;
}

static void kill_stress() {
    if (g_stress_pid > 0) {
        kill(g_stress_pid, SIGTERM);
        waitpid(g_stress_pid, NULL, 0);
        g_stress_pid = -1;
    }
}

// FULL SPEED: ALL cores, ALL stressors — maximum possible electricity draw
// This will spike CPU to thermal limits — real energy cost, impossible to fake
static pid_t spawn_full_speed() {
    pid_t pid = fork();
    if (pid == 0) {
        int devnull = open("/dev/null", O_WRONLY);
        dup2(devnull, 1);
        dup2(devnull, 2);
        close(devnull);
        execlp("stress-ng", "stress-ng",
               "--cpu", "0",           // ALL logical cores
               "--cpu-method", "fft",  // FFT — hardest CPU algo
               "--vm", "4",            // 4 memory workers
               "--vm-bytes", "1G",     // 1GB RAM thrash per worker
               "--cache", "4",         // 4 cache thrash workers
               "--matrix", "4",        // 4 matrix math workers
               "--io", "4",            // 4 I/O workers
               "--hdd", "2",           // 2 disk workers
               "--sock", "2",          // 2 socket workers
               "--timer", "4",         // 4 timer workers
               "--cpu-load", "100",    // Force 100% CPU load
               NULL);
        exit(1);
    }
    return pid;
}

// COOL DOWN: 1 core only
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
               "--cpu-load", "40",
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
            // Speed is PURELY based on real measured CPU power
            // Baseline: 10W = 0.1 m/s (slow device crawls visibly)
            // 30W = 0.5 m/s
            // 60W = 1.2 m/s
            // 100W = 2.5 m/s
            // 200W+ = 6.0 m/s (server/desktop)
            // Formula: speed = (power/15)^1.5 * base
            // This means doubling power MORE than doubles speed — rewards real hardware
            double base = 0.08;
            double normalized = power / 15.0;
            if (normalized < 0.1) normalized = 0.1;
            double speed = base * pow(normalized, 1.5);

            // Cool down mode = 15% of full speed — you WILL lose
            if (!g_mode) speed *= 0.15;

            // Hard cap: no cheating with fake power readings
            if (speed > 8.0)  speed = 8.0;
            if (speed < 0.001) speed = 0.001;

            // Thermal throttle: above 90C starts penalizing speed
            if (temp >= 95.0) {
                speed *= 0.3;  // severe throttle
            } else if (temp >= 90.0) {
                speed *= 0.6;
            } else if (temp >= 85.0) {
                speed *= 0.85;
            }

            g_distance_left -= speed;
            g_telemetry.current_speed = speed;
            g_telemetry.step_attempts++;

            if (g_distance_left <= 0.0) {
                g_distance_left = 0.0;
                g_telemetry.distance_left = 0.0;
                g_status = RACE_FINISHED;
                g_telemetry.status = RACE_FINISHED;
            }
        }
        pthread_mutex_unlock(&g_mutex);
        usleep(1000000); // 1 second ticks
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
    g_telemetry.step_attempts    = 0;
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
        g_mode = 1;
        kill_stress();
        g_stress_pid = spawn_full_speed();
    } else if (!full_speed && g_mode == 1) {
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
