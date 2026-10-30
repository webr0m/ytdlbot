# Webrom Downloader

Bot + Mini App en español e inglés, backend Go y frontend TypeScript. PostgreSQL local guarda cola, preferencias, cuotas y suscripciones. Los motores son yt-dlp, gallery-dl y HTTP directo. La interfaz principal es la Mini App; el bot entrega los archivos y abre la interfaz.

## Estado de esta instalación

- Mini App: https://api.webrom.cc
- Backend local: http://127.0.0.1:8095
- PostgreSQL del proyecto: Docker, puerto local 55435. No utiliza bases de otros proyectos.
- Administrador autorizado: **180791241** (configurado en `.env`, exento de cuotas).
- Cloudflare: sustituida la ruta antigua de `api.webrom.cc` por `http://127.0.0.1:8095`; el conector del proyecto usa `secrets/cloudflare-token.txt`. El túnel conserva su UUID anterior y las rutas ajenas al proyecto.
- Bot activo: [@mutidlprobot](https://t.me/mutidlprobot), mediante API local en loopback. Entrega real de vídeo YouTube comprobada.
- Acceso público por cuenta Telegram habilitado. Premium: 50 Stars/30 días, contacto de soporte mediante el perfil Telegram del administrador. Tu administrador no necesita pagar.
- Sesión MTProto del propietario iniciada localmente y verificada: autorizada, Premium y perteneciente al administrador. `MTPROTO_PATH` ya está conectado al backend y la entrega a Mensajes guardados se habilitó para el propietario. Aún no se ha validado una transferencia real desde un canal mediante esa sesión.
- Tipografía ampliada y temas Día/Noche con identidad negra y tonos neutros; selector persistente en este dispositivo.
- Control **Aa** en la cabecera: tamaño de letra entre 90 % y 130 %, guardado por dispositivo y botón para volver al tamaño normal. Formularios y textos se reorganizan al crecer.
- Perfil, comandos y Mini App en 🇪🇸 Español / 🇬🇧 English. Main Mini App habilitada en BotFather e icono negro/plata aplicado.
- No se han probado cobros/reembolsos reales.

## Activación

1. Las credenciales ya están en `.env` privado. Mantener `ADMIN_IDS=180791241`. En otra instalación, completar `BOT_TOKEN`, `BOT_USERNAME` (sin @), `TELEGRAM_API_ID`, `TELEGRAM_API_HASH`.
2. Ejecutar `pwsh -File scripts/enable-telegram.ps1`. Si el bot ya usaba la API cloud, detener su proceso anterior y usar `-MigrateFromCloud` una sola vez para realizar `logOut` antes de usar el servidor local.
3. Reiniciar: `pwsh -File scripts/stop.ps1`, luego `pwsh -File scripts/start.ps1 -Background`.
4. Si hace falta iniciar el conector: `pwsh -File scripts/start-tunnel.ps1`. No ejecutar dos conectores de este proyecto innecesariamente.
5. En @BotFather, configurar Main Mini App y el menú con `https://api.webrom.cc`. El servidor también configura el botón de menú por API al arrancar con token.
6. Abrir el bot, pulsar Iniciar y después abrir la Mini App. Telegram entrega `initData` firmado; el backend verifica firma, antigüedad e ID. No pide teléfono ni código a los clientes.
7. `PUBLIC_ACCESS=true` ya admite clientes con su propia cuenta Telegram. Ponerlo en false permite restringirlo al administrador y `ALLOWED_USER_IDS`.
8. `ENABLE_PAYMENTS=true` y `PREMIUM_STARS=50` ya están configurados. Soporte apunta al perfil del administrador (`tg://user?id=180791241`); puede cambiarse por `https://t.me/TU_SOPORTE`. Las condiciones se muestran en Ayuda. No se han realizado cobros desde esta instalación.

## Planes

| Función | Esencial | Premium del descargador | Administrador |
|---|---|---|---|
| YouTube | 5 tareas/día UTC | 100 tareas/período de 30 días | Sin cuota |
| Redes, galerías y archivos | 50 tareas/día UTC | 300 tareas/período | Sin cuota |
| Calidad | Hasta 1080p | Hasta 4K disponible en origen | Todas |
| Lotes, FLAC, subtítulos, recortes | No | Sí | Sí |
| Archivos grandes divididos | No | Sí | Sí |
| Transcodificación por RAM | No | Sí | Sí |
| Panel de recursos, pausa, reembolsos | No | No | Sí |

La suscripción del descargador es independiente de Telegram Premium. Se paga en Stars, se renueva cada 30 días y se cancela desde Cuenta. El backend valida la factura, usuario, moneda e importe y espera `successful_payment`; el callback visual de pago no concede permisos. Los identificadores únicos de cobro evitan activar dos veces el mismo pago. Los reembolsos revocan el período correspondiente.

## Archivos grandes y RAM

La entrega ahora es un **único archivo completo**. Las partes binarias son internas: se verifican tamaños y SHA-256, se leen secuencialmente y se suben como un solo flujo. No se genera otra copia unida en disco y el usuario no necesita reconstruir nada.

- Bot API local: límite oficial de 2000 MB, con margen configurado de 1900 MB.
- Propietario de la sesión Telegram Premium: archivos mayores se envían mediante MTProto a **sus propios Mensajes guardados**, hasta 4 GiB. Se comprueba que la sesión pertenece al ID destinatario y que sigue siendo Premium. No se usa la cuenta personal para entregar a otros usuarios.
- Una unión virtual conserva el tamaño original y no evita estos límites. Los archivos que exceden el transporte se rechazan con un error para reducir calidad o recortar; no se entregan partes automáticamente.

En modo RAM, FFmpeg produce un MP4 H.264/AAC fragmentado por una tubería. Se usan búferes de 1 MiB y bloques temporales en disco de hasta `RAM_PART_MB` MB para conocer el tamaño final y comprobar que FFmpeg terminó correctamente **antes de enviar**. Los bloques se leen como un único vídeo y se eliminan tras la entrega o el error. Se necesita espacio para la descarga original y los bloques convertidos; no hay una tercera copia unida. La codificación tiene pérdida.

`FFMPEG_ENCODER=auto` detecta AMD AMF, NVIDIA NVENC o Intel QSV y comprueba el archivo real antes de usar hardware. En este equipo se verificó AMD AMF y D3D11VA con Radeon RX 6600M. La alternativa es libx264. El historial distingue conversión, unión virtual y subida y conserva la confirmación de Telegram.

Para recuperar las partes **antiguas** ya enviadas, sigue disponible `node scripts/join-parts.mjs archivo.manifest.json`. El propietario también puede enviarlas como un único archivo, sin reconstruir otra copia:

```powershell
.\bin\mtproto.exe -owner 180791241 -upload-manifest "ruta\video.mp4.manifest.json"
```

`MTPROTO_SAVED_UPLOADS=true` habilita el transporte del propietario. `MTPROTO_UPLOAD_THREADS` controla de 1 a 8 solicitudes concurrentes de bloques MTProto de 512 KiB; no crea más trabajos de conversión. La sesión se verifica localmente y el mensaje final se vuelve a consultar para comprobar nombre y tamaño completos. Una interrupción durante la confirmación puede dejar una entrega exitosa sin registrar: revisar Mensajes guardados antes de reintentar.

## Cola y límites del equipo

- Concurrencia inicial de 2 tareas; el administrador puede cambiar entre 1 y 4 desde Cuenta → Estado del servidor, sin reiniciar. Se guarda en PostgreSQL. Reducir el límite permite finalizar las tareas activas y restringe las nuevas.
- Cada usuario tiene su propio espacio: `MAX_ACTIVE_PER_USER` (2 por defecto) permite dos descargas propias en paralelo sin que una sola cuenta acapare todos los trabajadores. Con muchos usuarios puede bajarse a 1 para repartir mejor los workers.
- El orden de la cola es justo: primero las tareas Premium, después las gratuitas antiguas (más de 30 minutos) para evitar inanición, y entre iguales se prioriza al usuario que no tiene otra tarea activa. Las tareas del mismo usuario conservan su orden de llegada.
- Una sola operación pesada de transcodificación o entrega grande a la vez, incluso al aumentar trabajadores. Las subidas normales comparten `MAX_CONCURRENT_UPLOADS` (2 por defecto) para no saturar Telegram ni dejar sin ancho de banda al bot.
- FFmpeg usa 2 hilos configurables; procesos en prioridad baja en Windows.
- Máximo 5 tareas pendientes por usuario, `MAX_QUEUE` tareas en toda la cola (1000 por defecto), máximo de 20 elementos por lote y retención de 24 h de archivos temporales. El historial permanece en PostgreSQL.
- Reserva de disco: 4096 MB; se detiene una tarea si cae por debajo.
- PostgreSQL usa un pool de 16 conexiones por defecto (`DB_MAX_CONNS`) para que los latidos, la API y el bot no compitan por cuatro conexiones. Los latidos de las tareas se escriben cada 15 s y la cancelación se comprueba cada 2 s.
- El polling del bot no se bloquea si un chat bloqueó al bot, una actualización es inválida o Telegram responde con error: se registra y se avanza el offset. Los mensajes con 429/5xx se reintentan con espera.
- Cancelación antes de la entrega; una subida iniciada no se cancela desde la interfaz para evitar entregas ambiguas. Si se interrumpe el servicio, revisar archivos recibidos antes de reintentar.
- Un bloqueo de PostgreSQL evita arrancar dos instancias del servicio con la misma base.

## MTProto opcional

Se usa **gotd/td en Go, sin TDLib**. Compilar:

```powershell
.\.tools\go\bin\go.exe build -o bin\mtproto.exe ./cmd/mtproto
.\bin\mtproto.exe -login
```

El propietario escribe teléfono, código y 2FA en su terminal local, sin eco. La sesión se guarda en `secrets/mtproto-session.json`, que equivale a una credencial de acceso. No se publica. Después configurar `MTPROTO_PATH=D:/Descargas/ytdlbot/bin/mtproto.exe` y reiniciar.

Este módulo permite al **administrador** descargar documentos/vídeos de enlaces `https://t.me/canal/mensaje` accesibles a su cuenta, con dos hilos. No se unen canales automáticamente ni se exporta contenido marcado como no reenviable. No se presta la sesión personal a clientes de la Mini App. Telegram Premium puede beneficiar descargas desde Telegram; no acelera YouTube ni convierte el bot en una cuenta Premium. La entrega a clientes continúa por Bot API local. La sesión ya se verificó; sigue pendiente una transferencia real desde un canal autorizado.

## Desarrollo y hosting

```powershell
pwsh -File scripts/setup.ps1
npm run build --prefix web
.\.tools\go\bin\go.exe test ./...
node --test scripts/join-parts.test.mjs
```

Go incluye la web compilada mediante `embed`. Para servidor Linux/VPS, copiar código y `.env` privado, después `docker compose --profile telegram --profile server up -d --build`. Se requieren procesos persistentes, disco, FFmpeg y Python/motores: un hosting PHP compartido o un Worker de Cloudflare por sí solos no ejecutan todo este sistema. La Mini App y sus JSON atraviesan Cloudflare; las subidas grandes van del equipo a Telegram.

La API Telegram y PostgreSQL solo escuchan en loopback. El panel local usa una clave independiente y rechaza acceso mediante cabeceras del túnel. La API web valida acceso por Telegram, limita solicitudes y separa trabajos por propietario. URL y DNS se validan contra redes privadas; los motores usan un proxy de salida local con resolución fijada a IP pública. En un hosting público se recomienda además aislamiento de red del worker y cuotas de disco por volumen.

Ver [investigación técnica](docs/INVESTIGACION.md) y [estado de validación](docs/VALIDACION.md).
