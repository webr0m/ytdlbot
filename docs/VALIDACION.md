# Validación · 30 de octubre de 2026

## Comprobado

- Build TypeScript/Vite de producción; web embebida en Go.
- `go test ./...`: autenticación Mini App (firma, campos duplicados, datos manipulados y vencidos); URLs/redes reservadas; argumentos de opciones; división exacta y límites de partes; parada ante error de subida.
- Integración PostgreSQL en esquema temporal aislado: límite concurrente de tareas, claim sin duplicación, prohibición de cancelar tareas ajenas, funciones Premium bloqueadas para plan gratis, activación idempotente y rechazo de importe incorrecto.
- FFmpeg real con vídeo sintético: salida MP4 por tubería RAM, transporte multipart simulado, partes y manifiesto coherentes.
- `node --test scripts/join-parts.test.mjs`: reconstrucción exacta, hashes, rechazo de corrupción, rutas fuera de carpeta y sobrescrituras.
- `go vet ./...`; auditoría npm sin vulnerabilidades notificadas al ejecutar.
- API Telegram local `getMe`: identidad @mutidlprobot verificada. Bot migrado desde API cloud con logOut; menú web_app apunta a https://api.webrom.cc/.
- HTTPS público y /healthz responden. Clave de administrador local rechazada desde internet (401). Cuenta administrativa local verificada como ID 180791241.
- Primera entrega real: tarea `ebfd736d0e78cd87d6e66ddb6575a93d`, vídeo YouTube NTCEC Intro by Webrom ft. kukushuku, MP4 de 3.115.614 bytes, estado done/100%, file_id devuelto por Telegram.
- Transcodificación RAM real: tarea `f5865a12bca654389f38005383817b91`, parte MP4 de 5.519.414 bytes y manifiesto de 534 bytes confirmados por Telegram, estado done. Una prueba anterior detectó selección de intermedios; se corrigió usando las rutas after_move de yt-dlp y resolución absoluta de FFmpeg. Se añadió una prueba de regresión que excluye intermedios y conserva subtítulos.
- Premium habilitado a 50 Stars/30 días; usuarios públicos entran mediante firma de Telegram. El administrador está excluido de la compra: el endpoint rechaza cobrarle porque ya tiene todas las funciones.
- Sitio revisado en Chrome. Captura: `artifacts/miniapp-webrom.png`.
- `.env`, `data`, `.tools`, binarios y `secrets` excluidos de Git. No se publicaron credenciales en repositorios.

## Límites de la evidencia

- No se ha realizado un cobro o reembolso real de Stars. La lógica de cuenta/pagos sí tiene pruebas locales; las operaciones de dinero necesitan una validación de extremo a extremo en Telegram antes de considerarlas comprobadas.
- El acceso firmado de Mini App se prueba criptográficamente; no se ha automatizado una sesión de compra móvil.
- No se hizo una transferencia de 2 GB real. Las pruebas pequeñas ejercitan la misma lógica de corte y hashes, sin consumir gigabytes innecesarios.
- MTProto/gotd compila y la sesión local del propietario fue verificada como autorizada/Premium y coincidente con ADMIN_IDS. El backend tiene conectado el ejecutable. Sigue pendiente validar una transferencia real desde un canal autorizado.
- No se comprobaron enlaces reales de Instagram, TikTok, Facebook, X o galerías en esta sesión; dependen del extractor, acceso y cookies requeridas por cada sitio.
- JDownloader y aria2 se investigaron, pero no están integrados. Esta versión utiliza yt-dlp, gallery-dl, HTTP y un módulo MTProto administrativo opcional.
- El equipo, Docker, el backend y el conector deben permanecer encendidos. No se configuró inicio automático de Windows.

## Rendimiento, envío y reconstrucción

- Se diagnosticó la aparente congelación: un vídeo VP9/Opus de 2 h 49 min y 1,4 GB estaba codificándose por CPU; la interfaz lo etiquetaba como envío y esperaba 512 MB para la primera entrega.
- Se verificó la Radeon RX 6600M mediante codificación AMD AMF y decodificación D3D11VA del archivo real. La nueva selección comprueba compatibilidad de la fuente y conserva alternativa CPU.
- La primera parte se reduce a 32 MiB; se informa por separado de conversión y bytes subidos. Las partes confirmadas se persisten inmediatamente.
- Se recuperaron de Telegram la parte y el manifiesto de la prueba NTCEC; `join-parts.mjs` reconstruyó el archivo, validó SHA-256 y FFmpeg decodificó el resultado correctamente.
- Nueva prueba Go → Node: 80 MiB en tres partes (primer bloque de 32 MiB), reconstrucción y contenido idénticos.
- Nueva prueba de integración PostgreSQL/worker/transporte: dos trabajos subieron simultáneamente a un servidor Telegram simulado con un solo bot. Se verificó admisión atómica y reducción del límite.
- Perfil, comandos y Main Mini App verificados. La interfaz permite 🇪🇸/🇬🇧 y conserva el idioma al recargar; se revisaron descarga y planes en inglés.
- **Verificación completa del vídeo largo:** tarea `916d1f7fd4ccc852e9cbfc04bde9a6c6`, estado done/100 %, nueve partes confirmadas por Telegram más manifiesto. Se recuperaron las nueve partes desde Telegram y se reconstruyó un MP4 de **3.953.704.982 bytes**. Coinciden los SHA-256 de cada parte y del archivo completo: `55e0697262c9b736cb69dbcc658069f811e83e88d2288566accf6185c19b29b7`. Evidencia local en `data/verified-telegram/916d1f7fd4ccc852e9cbfc04bde9a6c6/verification.json`.
- FFmpeg verificó decodificación al comienzo y cerca del final del MP4 reconstruido. Duración: **02:49:05.04**, vídeo H.264 1920×1080 a 30 fps y audio AAC. No se hizo una revisión humana completa de las 2 h 49 min.
- Concurrencia real configurada en 2 y endpoint administrativo comprobado. El límite inválido de 5 tareas fue rechazado; la cola quedó sin tareas activas ni pendientes. Cambios de concurrencia no interrumpen tareas activas.
- El reconstructor muestra avance por parte y usa bloques de E/S de 4 MiB manteniendo memoria acotada. Comprobaciones de hashes y rechazo de sobrescritura siguen pasando.

## Revisión visual posterior

Se amplió la tipografía de navegación, formularios, botones y ayuda. Se añadieron temas Día/Noche persistentes por dispositivo y se sustituyó la paleta verde por negro, blancos y grises. Compilación TypeScript/Vite correcta; se comprobaron ambos temas en la web pública y la conservación de la elección al recargar. Capturas definitivas: `artifacts/miniapp-dia-negro.png` y `artifacts/miniapp-noche-negro.png`.

Se añadió el control **Aa**, escala 90–130 %, memoria por dispositivo y restauración al 100 %. La tipografía usa unidades rem y los contenedores pueden crecer/reorganizarse. Se revisó el 130 % en escritorio y en viewport móvil de 390 px; las mediciones dieron ancho de contenido igual al ancho útil, sin scroll horizontal (375/375 en móvil, 1673/1673 en escritorio). El logotipo conserva su tamaño para evitar que su identidad se parta. Captura: `artifacts/miniapp-ajuste-fuente.png`. La actualización se aplicó cuando terminó la descarga activa, sin interrumpirla.


## Entrega como archivo único — 2026-10-30

- Se sustituyó la entrega de bloques binarios por unión virtual. Los bloques son internos y se envía un único archivo; las partes antiguas siguen recuperables mediante el manifiesto.
- `internal/virtualfile`: comprobación previa de tamaños, SHA-256 por parte y total, nombres seguros, cancelación y límite final. Lectura secuencial con un archivo abierto a la vez y búfer de 1 MiB; no se crea una copia unida adicional.
- Pruebas automáticas: bloques de distintos tamaños, partes ausentes/corruptas/duplicadas, rutas fuera de la carpeta, límite exacto y excedido; transporte multipart recibe exactamente el contenido original en un solo archivo.
- FFmpeg real: produce un MP4 completo, se envía una sola vez como vídeo, se decodifica correctamente y se limpian los bloques internos. Una conversión fallida no inicia ninguna carga.
- Aislamiento MTProto: el backend solo autoriza Mensajes guardados para administradores en su chat propio y la herramienta exige que el ID destinatario coincida con el propietario real de la sesión. Se vuelve a comprobar Telegram Premium y el tamaño máximo antes de cargar.
- Entrega real pequeña en Mensajes guardados: 3.115.614 bytes; ID 687416. Telegram confirmó nombre y tamaño completos.
- Entrega real por el bot desde el flujo nuevo: tarea `a11bad1bfa0f73ef48533d3961b52921`, un vídeo de 5.695.759 bytes, estado done/100 %, sin partes ni manifiesto enviados.
- `npm run build --prefix web`, `go test ./...` y `go vet ./...` aprobados. Mini App pública responde 200 y expone el paquete nuevo. La API local informa `savedUploads=true` para el propietario.
- La carga de 3.953.704.982 bytes a Mensajes guardados sigue en comprobación. No se da por confirmada hasta releer el mensaje final desde Telegram.

## Concurrencia por usuario, cola justa y resiliencia — 2026-10-30

- `MAX_ACTIVE_PER_USER` (2 por defecto, configurable 1–4) limita las tareas activas de cada cuenta dentro de una transacción con bloqueo de asesoría de PostgreSQL. Dos workers que llaman a la cola a la vez no pueden superar el cupo de un mismo usuario.
- Orden de la cola verificado con pruebas de integración en esquema temporal: primero Premium y administradores, después tareas gratuitas con más de 30 minutos de espera (anti-inanición), después usuarios sin otra tarea activa y, por último, orden de llegada. Una tarea gratuita no adelanta a una Premium; un usuario no acapara dos espacios mientras otro espera; el administrador recibe prioridad.
- Se añadieron índices parciales para consultar tareas activas, latidos y retención (`jobs_user_active`, `jobs_active_heartbeat`, `jobs_finished_retention`), evitando escaneos completos al crecer el historial.
- `MAX_CONCURRENT_UPLOADS` (2 por defecto) separa la concurrencia de descargas de la de entregas. La prueba de integración con un solo bot y dos workers confirma que dos trabajos suben a la vez al servidor Telegram simulado sin bloquearse.
- Los latidos se escriben cada 15 s y la cancelación se consulta cada 2 s. Un fallo transitorio de base de datos ya no cancela la descarga: se toleran hasta cinco errores consecutivos.
- El polling del bot registra y descarta una actualización fallida, avanza el offset y reintenta errores 429/5xx de Telegram con espera. Un chat bloqueado o una carga inválida ya no congela los mensajes siguientes.
- `go build ./...`, `go vet ./...` y `go test ./... -count=1` aprobados tras los cambios.
- Límite de la evidencia: no se ejecutó una prueba de carga con varios usuarios reales simultáneos; la corrección del cupo y del reparto se comprobó con workers concurrentes contra PostgreSQL y con el transporte Telegram simulado.
- Despliegue del 30-10-2026: binario y recursos web reconstruidos y servicio reiniciado. `GET /api/system` confirma `workers=2`, `activePerUser=2` y `uploads=2`; `@mutidlprobot` responde `getMe` y `getWebhookInfo` no muestra webhook ni errores de polling.

## Conversión de vídeo solo cuando hace falta — 2026-10-30

- `internal/app/compatibility.go`: `telegramVideoCompatible` lee solo las cabeceras con FFmpeg y acepta la entrega directa cuando el archivo es MP4 con vídeo H.264, píxel `yuv420p` y audio AAC (o sin audio). Cualquier otro contenedor, códec o formato de píxel requiere conversión.
- `shouldTranscodeVideo` concentra la decisión: la opción de conversión activa ya no implica recodificar siempre; solo se convierte si el archivo descargado no es compatible.
- `videoFormat` prefiere siempre `avc` + `mp4a` en yt-dlp, de modo que YouTube a 1080p o menos se entrega sin recodificar. Si no existe ese par (por ejemplo 4K), se usa la mejor fuente disponible y la comprobación decide.
- Pruebas aprobadas: `TestTelegramVideoCompatible` (H.264/AAC, sin audio, códec distinto, audio distinto y H.264 de 10 bits), `TestShouldTranscodeOnlyWhenNeeded` (compatible, incompatible y opción desactivada) y `TestVideoFormatPrefersH264AAC`.
- `npm run build --prefix web`, `go build ./...`, `go vet ./...` y `go test ./... -count=1` aprobados con el código nuevo.
- Límite de la evidencia: la comprobación usa la cabecera que escribe FFmpeg, no un análisis frame a frame; un archivo con metadatos engañosos podría requerir conversión tras el rechazo de Telegram.
