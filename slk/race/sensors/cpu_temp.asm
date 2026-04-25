; SLK CPU Temperature Reader - x86_64 Linux Assembly
; Reads /sys/class/thermal/thermal_zone13/temp (x86_pkg_temp - real CPU package temp)
; Returns temperature in millidegrees in rax

section .data
    thermal_path db "/sys/class/thermal/thermal_zone13/temp", 0
    buf          db 16 dup(0)

section .text
    global slk_read_cpu_temp_asm

slk_read_cpu_temp_asm:
    ; open("/sys/class/thermal/thermal_zone13/temp", O_RDONLY)
    mov rax, 2
    lea rdi, [rel thermal_path]
    xor rsi, rsi
    xor rdx, rdx
    syscall

    cmp rax, 0
    jl  .error

    mov r8, rax

    ; read(fd, buf, 16)
    mov rax, 0
    mov rdi, r8
    lea rsi, [rel buf]
    mov rdx, 16
    syscall

    ; close(fd)
    push rax
    mov rax, 3
    mov rdi, r8
    syscall
    pop rcx

    ; Convert ASCII to integer (millidegrees)
    lea rsi, [rel buf]
    xor rax, rax
    xor rcx, rcx

.parse_loop:
    movzx rcx, byte [rsi]
    cmp cl, '0'
    jl  .done
    cmp cl, '9'
    jg  .done
    imul rax, rax, 10
    sub rcx, '0'
    add rax, rcx
    inc rsi
    jmp .parse_loop

.done:
    ret

.error:
    mov rax, 45000
    ret

section .note.GNU-stack noalloc noexec nowrite progbits
