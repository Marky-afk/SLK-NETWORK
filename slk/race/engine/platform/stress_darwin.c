#include <stdlib.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/wait.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>

static pid_t g_pid = -1;

void platform_kill_stress() {
    if (g_pid > 0) {
        kill(g_pid, SIGTERM);
        waitpid(g_pid, NULL, 0);
        g_pid = -1;
    }
}

// macOS uses 'yes' piped to /dev/null as CPU stress
int platform_spawn_full_speed() {
    pid_t pid = fork();
    if (pid == 0) {
        int devnull = open("/dev/null", O_WRONLY);
        dup2(devnull, 1);
        dup2(devnull, 2);
        close(devnull);
        // Use 4 yes processes for 4 cores
        execlp("sh", "sh", "-c",
               "yes > /dev/null & yes > /dev/null & yes > /dev/null & yes > /dev/null",
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
        execlp("sh", "sh", "-c", "yes > /dev/null", NULL);
        exit(1);
    }
    g_pid = pid;
    return pid > 0 ? 0 : -1;
}

// macOS reads CPU power from powermetrics
double platform_read_cpu_power() {
    FILE* f = popen("powermetrics -n 1 -i 500 --samplers cpu_power 2>/dev/null | grep 'CPU Power' | awk '{print $3}'", "r");
    if (!f) return 10.0;
    double watts = 10.0;
    fscanf(f, "%lf", &watts);
    pclose(f);
    if (watts < 1.0 || watts > 300.0) return 10.0;
    return watts;
}

// macOS notification via osascript
void platform_notify(const char* title, const char* msg) {
    char cmd[512];
    snprintf(cmd, sizeof(cmd),
        "osascript -e 'display notification \"%s\" with title \"%s\"' &",
        msg, title);
    system(cmd);
}
