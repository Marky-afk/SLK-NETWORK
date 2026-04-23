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

double platform_read_cpu_power() {
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

void platform_notify(const char* title, const char* msg) {
    char cmd[512];
    snprintf(cmd, sizeof(cmd), "notify-send '%s' '%s' --urgency=critical &", title, msg);
    system(cmd);
}
