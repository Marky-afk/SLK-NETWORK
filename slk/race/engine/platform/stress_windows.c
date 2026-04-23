#ifdef _WIN32
#include <windows.h>
#include <stdio.h>

static HANDLE g_process = NULL;

void platform_kill_stress() {
    if (g_process != NULL) {
        TerminateProcess(g_process, 0);
        CloseHandle(g_process);
        g_process = NULL;
    }
}

// Windows: run a CPU stress loop using a .bat script
int platform_spawn_full_speed() {
    // Write stress batch file
    FILE* f = fopen("C:\\slk_stress.bat", "w");
    if (!f) return -1;
    fprintf(f, "@echo off\n");
    fprintf(f, ":loop\n");
    fprintf(f, "for /L %%i in (1,1,10000) do set /a x=%%i*%%i > nul\n");
    fprintf(f, "goto loop\n");
    fclose(f);

    STARTUPINFO si = {0};
    PROCESS_INFORMATION pi = {0};
    si.cb = sizeof(si);
    si.dwFlags = STARTF_USESHOWWINDOW;
    si.wShowWindow = SW_HIDE;

    if (!CreateProcess(NULL, "cmd.exe /c C:\\slk_stress.bat",
        NULL, NULL, FALSE, CREATE_NO_WINDOW, NULL, NULL, &si, &pi)) {
        return -1;
    }
    g_process = pi.hProcess;
    CloseHandle(pi.hThread);
    return 0;
}

int platform_spawn_cool_down() {
    platform_kill_stress();
    // Same but slower
    FILE* f = fopen("C:\\slk_stress_cool.bat", "w");
    if (!f) return -1;
    fprintf(f, "@echo off\n");
    fprintf(f, ":loop\n");
    fprintf(f, "for /L %%i in (1,1,1000) do set /a x=%%i*%%i > nul\n");
    fprintf(f, "ping -n 1 127.0.0.1 > nul\n");
    fprintf(f, "goto loop\n");
    fclose(f);

    STARTUPINFO si = {0};
    PROCESS_INFORMATION pi = {0};
    si.cb = sizeof(si);
    si.dwFlags = STARTF_USESHOWWINDOW;
    si.wShowWindow = SW_HIDE;

    CreateProcess(NULL, "cmd.exe /c C:\\slk_stress_cool.bat",
        NULL, NULL, FALSE, CREATE_NO_WINDOW, NULL, NULL, &si, &pi);
    g_process = pi.hProcess;
    CloseHandle(pi.hThread);
    return 0;
}

// Windows reads CPU power from WMI
double platform_read_cpu_power() {
    // Use RAPL via registry — simplified fallback to 15W estimate
    // Full WMI implementation requires COM initialization
    return 15.0;
}

// Windows notification via PowerShell toast
void platform_notify(const char* title, const char* msg) {
    char cmd[512];
    snprintf(cmd, sizeof(cmd),
        "powershell -Command \"Add-Type -AssemblyName System.Windows.Forms; "
        "[System.Windows.Forms.MessageBox]::Show('%s','%s')\" &",
        msg, title);
    system(cmd);
}
#endif
