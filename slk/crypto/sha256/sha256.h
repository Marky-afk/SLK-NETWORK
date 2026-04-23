#ifndef SLK_SHA256_H
#define SLK_SHA256_H

#include <stdint.h>
#include <stddef.h>

// Compute SHA-256 hash
// data: input bytes
// len:  input length
// out:  32-byte output buffer
void slk_sha256(const uint8_t *data, size_t len, uint8_t *out);

// Assembly optimized block processor
void slk_sha256_block(uint32_t state[8], const uint8_t block[64]);

#endif
