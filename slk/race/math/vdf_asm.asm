; SLK VDF Assembly helpers
; Provides fast XOR mixing between SHA-256 rounds

section .text
    global vdf_xor_mix

; void vdf_xor_mix(uint8_t* buf, uint64_t iteration)
; Mixes iteration counter into buffer to prevent optimization
vdf_xor_mix:
    ; rdi = buf pointer
    ; rsi = iteration number
    mov rax, rsi
    xor [rdi],    al
    shr rax, 8
    xor [rdi+8],  al
    shr rax, 8
    xor [rdi+16], al
    shr rax, 8
    xor [rdi+24], al
    ret

section .note.GNU-stack noalloc noexec nowrite progbits
