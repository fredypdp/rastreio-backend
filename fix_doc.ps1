# Read the file as UTF-8
$content = Get-Content -Path "C:\Users\FRED\Desktop\Dev\Spuri\SpuriBackend\docs\Debbugs\seguranca-e-migracao-brevo.md" -Raw -Encoding UTF8

# Fix the status section in Parte 1
$oldStatus = '@"
# Parte 1 â€" DiagnÃ³stico de seguranÃ§a (resolvido)

> **Estado desta parte (14/11/2026):** SEC-01 a SEC-15 estÃ£o **resolvidos**. SEC-01 a SEC-07 pela Tarefa 123 do `rastreio-backend` e Tarefa 34 do `rastreio-frontend`. SEC-08 a SEC-15 pela Tarefa 124 do `rastreio-backend` (este repositÃ³rio). O mecanismo `sessoes_validas_desde` de **SEC-10** foi entregue como dependÃªncia do reenvio de credenciais (reset por link e reenvio). **Todas as partes (1 a 8) estÃ£o CONCLUÃDAS ✅**.
> O tÃ­tulo "(resolvido)" foi aplicado a pedido; leia-o como "SEC-01 a SEC-15 resolvidos no backend".
>
> **Todas as partes concluÃ­das (14/11/2026):**
> - **Parte 2 (eliminar EmailJS/SMTP/gerar-token):** ✅ backend (Tarefa 123 `rastreio-backend`) + ✅ frontend (Tarefa 35 `rastreio-frontend`).
> - **Parte 3 (credenciais temporÃ¡rias D2/D3):** ✅ backend (Tarefa 123 `rastreio-backend`) + ✅ frontend (Tarefa 34 `rastreio-frontend`).
> - **Parte 4 (recuperaÃ§Ã£o por link, opÃ§Ã£o B):** ✅ backend (Tarefa 123 `rastreio-backend`) + ✅ frontend (jÃ¡ usa `emailAuthService`).
> - **Parte 6 (login por estado da conta D4):** ✅ backend (Tarefa 124 `rastreio-backend`) + ✅ frontend (pendente: Tarefa do `rastreio-frontend` para remover sessÃ£o restrita e avisos de exclusÃ£o).
> - **Partes 7 e 8 (rate limiters e ordem de execuÃ§Ã£o):** ✅ backend (Tarefa 124 `rastreio-backend` para Parte 7; Parte 8 Ã© documento de referência).
"@'

$newStatus = '@"
# Parte 1 — Diagnóstico de segurança (resolvido)

> **Estado desta parte (14/11/2026):** SEC-01 a SEC-15 estão **resolvidos**. SEC-01 a SEC-07 pela Tarefa 123 do `rastreio-backend` e Tarefa 34 do `rastreio-frontend`. SEC-08 a SEC-15 pela Tarefa 124 do `rastreio-backend` (este repositório). O mecanismo `sessoes_validas_desde` de **SEC-10** foi entregue como dependência do reenvio de credenciais (reset por link e reenvio). **Todas as partes (1 a 8) estão CONCLUÍDAS ✅**.
> O título "(resolvido)" foi aplicado a pedido; leia-o como "SEC-01 a SEC-15 resolvidos no backend".
>
> **Todas as partes concluídas (14/11/2026):**
> - **Parte 2 (eliminar EmailJS/SMTP/gerar-token):** ✅ backend (Tarefa 123 `rastreio-backend`) + ✅ frontend (Tarefa 35 `rastreio-frontend`).
> - **Parte 3 (credenciais temporárias D2/D3):** ✅ backend (Tarefa 123 `rastreio-backend`) + ✅ frontend (Tarefa 34 `rastreio-frontend`).
> - **Parte 4 (recuperação por link, opção B):** ✅ backend (Tarefa 123 `rastreio-backend`) + ✅ frontend (já usa `emailAuthService`).
> - **Parte 6 (login por estado da conta D4):** ✅ backend (Tarefa 124 `rastreio-backend`) + ✅ frontend (pendente: Tarefa do `rastreio-frontend` para remover sessão restrita e avisos de exclusão).
> - **Partes 7 e 8 (rate limiters e ordem de execução):** ✅ backend (Tarefa 124 `rastreio-backend` para Parte 7; Parte 8 é documento de referência).
"@'

$content = $content -replace [regex]::Escape($oldStatus), $newStatus

# Fix SEC-15 header
$oldSEC15Header = '## SEC-15 â€" Isolamento entre academias: a atual lÃª registos das anteriores e a anterior perde o acesso (MÃ©dia) â€" âœ… resolvido'
$newSEC15Header = '## SEC-15 — Isolamento entre academias: a atual lê registos das anteriores e a anterior perde o acesso (Média) — ✅ **CONCLUÍDO**'
$content = $content -replace [regex]::Escape($oldSEC15Header), $newSEC15Header

# Fix "Implementado" text in SEC-15
$oldImpl = '**Implementado (Partes 6.5.1/6.5.2/6.5.3):**'
$newImpl = '**✅ Implementado (Partes 6.5.1/6.5.2/6.5.3) — Tarefa 125:**'
$content = $content -replace [regex]::Escape($oldImpl), $newImpl

# Fix the corrupted UTF-8 characters throughout the document
# Replace common UTF-8 misinterpreted as Latin-1 sequences
$replacements = @{
    'Ã£' = 'ã'
    'Ã¡' = 'á'
    'Ã©' = 'é'
    'Ã­' = 'í'
    'Ã³' = 'ó'
    'Ãº' = 'ú'
    'Ã§' = 'ç'
    'Ãª' = 'ê'
    'Ã´' = 'ô'
    'Ãµ' = 'õ'
    'Ã€' = 'À'
    'Ã‰' = 'É'
    'Ã" ' = 'Ó '
    'Ãœ' = 'Ü'
    'Ãƒ' = 'Ã'
    'Ã‡' = 'Ç'
    'Ã' = 'Í'
    'Ã'' = 'í'
    'â€" ' = '— '
    'â€œ' = '"'
    'â€' = '"'
    'â€™' = ''
    'â€˜' = '''
    'âœ…' = '✅'
    'Ã—' = '×'
    'Ã±' = 'ñ'
}

foreach ($pair in $replacements.GetEnumerator()) {
    $content = $content -replace [regex]::Escape($pair.Key), $pair.Value
}

# Write back as UTF-8
Set-Content -Path "C:\Users\FRED\Desktop\Dev\Spuri\SpuriBackend\docs\Debbugs\seguranca-e-migracao-brevo.md" -Value $content -Encoding UTF8

Write-Host "Document fixed successfully!"