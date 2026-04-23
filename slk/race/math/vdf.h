#ifndef SLK_VDF_H
#define SLK_VDF_H

#include <stdint.h>
#include <stddef.h>

// VDF parameters
#define VDF_ITERATIONS 100000  // iterations per meter
#define VDF_HASH_SIZE  32      // SHA-256 output size

typedef struct {
    uint8_t input[32];   // seed/input
    uint8_t output[32];  // proof output
    uint64_t iterations; // how many iterations done
    double   time_taken; // seconds taken
} VDFProof;

// Compute VDF proof (slow - takes real time)
int  vdf_prove(const uint8_t* seed, uint64_t iterations, VDFProof* proof);

// Verify VDF proof (fast - 0.001 seconds)
int  vdf_verify(const VDFProof* proof);

// ASM optimized SHA-256 round
void vdf_sha256_asm(const uint8_t* input, uint8_t* output);

#endif
