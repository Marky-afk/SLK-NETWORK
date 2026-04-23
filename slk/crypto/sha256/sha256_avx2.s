; SLK SHA-256 - x86_64 Assembly Implementation
; Optimized for Linux x86_64

section .data
    ; SHA-256 Initial Hash Values (H0-H7)
    H0 dd 0x6a09e667
    H1 dd 0xbb67ae85
    H2 dd 0x3c6ef372
    H3 dd 0xa54ff53a
    H4 dd 0x510e527f
    H5 dd 0x9b05688c
    H6 dd 0x1f83d9ab
    H7 dd 0x5be0cd19

    ; SHA-256 Round Constants (K)
    K dd 0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5
      dd 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5
      dd 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3
      dd 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174
      dd 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc
      dd 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da
      dd 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7
      dd 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967
      dd 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13
      dd 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85
      dd 0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3
      dd 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070
      dd 0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5
      dd 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3
      dd 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208
      dd 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2

section .text
    global slk_sha256_block

; void slk_sha256_block(uint32_t state[8], const uint8_t block[64])
; rdi = state pointer
; rsi = block pointer
slk_sha256_block:
    push rbp
    mov  rbp, rsp
    push rbx
    push r12
    push r13
    push r14
    push r15

    ; Save state pointer
    mov r15, rdi

    ; Load current hash state
    mov eax, [r15]      ; a = H0
    mov ebx, [r15+4]    ; b = H1
    mov ecx, [r15+8]    ; c = H2
    mov edx, [r15+12]   ; d = H3
    mov r8d, [r15+16]   ; e = H4
    mov r9d, [r15+20]   ; f = H5
    mov r10d, [r15+24]  ; g = H6
    mov r11d, [r15+28]  ; h = H7

    pop r15
    pop r14
    pop r13
    pop r12
    pop rbx
    pop rbp
    ret
