#include <stdlib.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/wait.h>
#include <signal.h>

static pid_t g_pid = -1;

void platform_kill_stress() {
    if (g_pid > 0) {
        kill(g_pid, SIGTERM);
        waitpid(g_pid, NULL, 0);
        g_pid = -1;
    }
}

int platform_spawn_full_speed() {
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
    g_pid = pid;
    return pid > 0 ? 0 : -1;
}

int platform_spawn_cool_down() {
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
    g_pid = pid;
    return pid > 0 ? 0 : -1;
}

static double read_cpu_usage_percent() {
    // Read CPU usage from /proc/stat — works without root
    unsigned long long u1[4], u2[4];
    FILE *f = fopen("/proc/stat", "r");
    if (!f) return 50.0;
    fscanf(f, "cpu %llu %llu %llu %llu", &u1[0], &u1[1], &u1[2], &u1[3]);
    fclose(f);
    usleep(200000); // 200ms sample
    f = fopen("/proc/stat", "r");
    if (!f) return 50.0;
    fscanf(f, "cpu %llu %llu %llu %llu", &u2[0], &u2[1], &u2[2], &u2[3]);
    fclose(f);
    unsigned long long idle1 = u1[3];
    unsigned long long idle2 = u2[3];
    unsigned long long total1 = u1[0]+u1[1]+u1[2]+u1[3];
    unsigned long long total2 = u2[0]+u2[1]+u2[2]+u2[3];
    unsigned long long dtotal = total2 - total1;
    unsigned long long didle  = idle2  - idle1;
    if (dtotal == 0) return 50.0;
    return 100.0 * (1.0 - (double)didle / (double)dtotal);
}

double platform_read_cpu_power() {
    // Try RAPL first (most accurate)
    const char* paths[] = {
        "/sys/class/powercap/intel-rapl:0/energy_uj",
        "/sys/class/powercap/intel-rapl/intel-rapl:0/energy_uj",
        NULL
    };
    for (int i = 0; paths[i]; i++) {
        unsigned long long e1 = 0, e2 = 0;
        FILE *f = fopen(paths[i], "r");
        if (!f) continue;
        fscanf(f, "%llu", &e1);
        fclose(f);
        usleep(200000);
        f = fopen(paths[i], "r");
        if (!f) continue;
        fscanf(f, "%llu", &e2);
        fclose(f);
        double watts = (double)(e2 - e1) / 200000.0;
        if (watts >= 1.0 && watts <= 300.0) return watts;
    }
    // Fallback: estimate power from CPU usage via /proc/stat
    // Assumes TDP of 35W for laptop (HP EliteBook)
    double usage = read_cpu_usage_percent();
    double tdp   = 35.0; // watts at 100% load
    double idle  = 5.0;  // watts at idle
    return idle + (tdp - idle) * (usage / 100.0);
}

void platform_notify(const char* title, const char* msg) {
    char cmd[512];
    snprintf(cmd, sizeof(cmd), "notify-send '%s' '%s' --urgency=critical &", title, msg);
    system(cmd);
}
