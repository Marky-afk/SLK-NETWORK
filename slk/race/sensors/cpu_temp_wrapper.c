#include <stdio.h>

// Declared in ASM
extern long slk_read_cpu_temp_asm(void);

// Returns real CPU temp in Celsius
double slk_get_cpu_temp(void) {
    long millideg = slk_read_cpu_temp_asm();
    if (millideg <= 0 || millideg > 105000)
        return 45.0;
    return millideg / 1000.0;
}
