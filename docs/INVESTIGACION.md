# Investigación y decisiones · 30 de octubre de 2026

## Go frente a TypeScript

Elegimos Go para API, PostgreSQL, procesos, cola y transporte Telegram: distribución mediante ejecutable y concurrencia sin un servicio Node obligatorio para la API. TypeScript se usa donde aporta más: Mini App web. Es una elección de arquitectura, no una afirmación de que Go siempre utilice menos recursos. Los motores, FFmpeg y el volumen de descargas dominarán el consumo. Node también está instalado porque yt-dlp necesita un runtime JavaScript para soporte completo de YouTube.

Fuentes: [Go FAQ](https://go.dev/doc/faq), [TypeScript en Node](https://nodejs.org/learn/typescript/run-natively), [dependencias de yt-dlp](https://github.com/yt-dlp/yt-dlp#dependencies).

## Referencias revisadas

| Proyecto | Utilidad y decisión |
|---|---|
| [yt-dlp](https://github.com/yt-dlp/yt-dlp) | Motor principal de vídeo/audio, formatos, listas, subtítulos y recortes. Compatibilidad variable por sitio. |
| [gallery-dl](https://github.com/mikf/gallery-dl) | Motor complementario para galerías y colecciones. |
| [yt-dlg](https://github.com/oleksis/youtube-dl-gui) | Interfaz de escritorio, no motor independiente ni backend Telegram. |
| [cobalt](https://github.com/imputnet/cobalt) | Interfaz y API social útiles como referencia; no reemplaza todas las opciones de yt-dlp. |
| [MeTube](https://github.com/alexta69/metube) | Referencia de cola e interfaz web autoalojada sobre yt-dlp. |
| [sdp-downloader](https://github.com/TameyAnderson/sdp-downloader) | Referencia cercana: bot, Mini App y API local; usa Python/SQLite, distinta arquitectura a la solicitada. |
| [nonoo/yt-dlp-telegram-bot](https://github.com/nonoo/yt-dlp-telegram-bot) | Referencia Go y MTProto para integración con yt-dlp. |
| [aria2](https://github.com/aria2/aria2) | Descargador multiprotocolo potente; el alcance inicial usa HTTP directo para reducir servicios. No se integraron torrents. |
| [JDownloader API](https://my.jdownloader.org/developers/) | Aporta soporte de hosters, cuentas y desafíos; requiere Java y un adaptador específico. Evaluado, no integrado en esta versión. No se reutilizó su API local obsoleta sin autenticación. |

No se copió código de estos proyectos. Cada motor conserva su licencia y se instala como programa externo.

## Bot de referencia

Se revisó la ficha de [@XDownloader_pro_bot](https://t.me/XDownloader_pro_bot) y la conversación ya abierta en Telegram Desktop, sin realizar compras. Se observaron comandos, cuotas, ayuda anunciada, entregas y errores. El menú ofrecía Premium por 50 Stars. Había mensajes contradictorios Gratis/Premium; no se infirió su implementación interna ni se comprobó su promesa de 4 GB. La implementación nueva centraliza todas las elecciones en la Mini App.

## Varios bots frente a varios workers

La pregunta es si conviene añadir más bots de Telegram para que descarguen y suban en paralelo. La conclusión para este proyecto es **no por ahora**: el bot no es el cuello de botella.

- **Descargar no depende del token del bot.** yt-dlp, gallery-dl, FFmpeg y el transporte MTProto son procesos del servidor. Añadir bots no aumenta CPU, disco ni ancho de banda. Para descargas, la palanca correcta son más workers (procesos) sobre la misma cola de PostgreSQL, no más tokens.
- **Una subida concurrente ya funciona con un solo bot.** El Bot API local admite hasta 2000 MB por archivo y atiende varias peticiones a la vez. Este proyecto limita las subidas con `MAX_CONCURRENT_UPLOADS` precisamente para proteger el enlace y el bot.
- **Los límites de mensajería no desaparecen con el servidor local.** Telegram mantiene aproximadamente 30 mensajes/s por bot y recomienda no superar 1 mensaje/s por chat. Con 4 tareas activas y una notificación por tarea, un bot queda muy lejos de ese límite. Varios tokens podrían sumar cuota global, pero cada bot solo puede escribir a usuarios que hayan iniciado ese bot; además, los `file_id` son exclusivos de cada bot y no se pueden transferir entre ellos. Obligar a cada usuario a iniciar varios bots empeora la experiencia y complica pagos, cuotas y soporte.
- **Más cuentas MTProto no son una solución segura.** Cada sesión adicional exige otra cuenta y multiplica el riesgo de `FLOOD_WAIT` o restricciones; no se debe usar una cuenta personal para entregar archivos a clientes.
- **Escalar workers sí tiene sentido en un VPS o varias máquinas.** La cola ya usa `FOR UPDATE SKIP LOCKED` y un bloqueo de instancia. El siguiente paso, si hiciera falta, sería separar el proceso que atiende Telegram del proceso que ejecuta trabajos, para poder arrancar varios workers sin duplicar el polling del bot. En un solo equipo, subir de 4 workers suele empeorar el rendimiento porque compiten por disco, CPU, GPU y subida.

Recomendación: mantener **un bot principal** y escalar primero la cola y los workers. Solo tendría sentido un pool de bots de entrega si en el futuro se superan de forma sostenida los límites por bot o se separan funciones (por ejemplo, un bot de interacción y otro de entrega con consentimiento explícito del usuario). No se implementa ahora porque añade complejidad sin mejorar el caso actual.

Fuentes: [límites de bots](https://core.telegram.org/bots/faq#my-bot-is-hitting-limits-how-do-i-avoid-this), [servidor local del Bot API](https://core.telegram.org/bots/api#using-a-local-bot-api-server), [envío de archivos y `file_id`](https://core.telegram.org/bots/api#sending-files).

## Telegram, pagos y límites

- [API local oficial](https://core.telegram.org/bots/api#using-a-local-bot-api-server): hasta 2000 MB de subida. Las partes se limitan a 1900 MB.
- [Firma de Mini Apps](https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app): validar initData en servidor, nunca confiar en initDataUnsafe para conceder acceso.
- [Stars](https://core.telegram.org/bots/payments-stars): compras de servicios digitales dentro de Telegram, con soporte de pagos y reembolsos.
- [Suscripciones](https://core.telegram.org/bots/api#createinvoicelink): período de 2592000 segundos; cancelación con editUserStarSubscription.
- [gotd/td](https://github.com/gotd/td): cliente MTProto Go; evita depender de una biblioteca nativa TDLib.
- [Telegram Premium](https://telegram.org/faq_premium): beneficios de una sesión de usuario; no se transfieren al token del bot ni a descargas de otros sitios.
- [FFmpeg pipes](https://ffmpeg.org/ffmpeg-protocols.html#pipe): base de la transcodificación mediante entrada/salida en flujo.

Las comprobaciones hechas sobre el equipo y Cloudflare corresponden a esta sesión; no garantizan que el equipo permanezca encendido ni que el túnel siga conectado tras un reinicio.
