#ifndef SLK_PLATFORM_H
#define SLK_PLATFORM_H

// Platform-agnostic interface
// Each OS implements these same functions differently

void   platform_kill_stress();
int    platform_spawn_full_speed();
int    platform_spawn_cool_down();
double platform_read_cpu_power();
void   platform_notify(const char* title, const char* msg);

#endif
