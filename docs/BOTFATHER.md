# Presentación y ajustes de Telegram

Bot: **@mutidlprobot** · Nombre: **Multi Downloader Pro**.

- Icono negro/plata generado mediante ImageGen y aplicado con `setMyProfilePhoto`.
- Descripción larga y breve registradas para `es`, `en` y una alternativa bilingüe. Símbolos: 🇪🇸 Español y 🇬🇧 English.
- Comandos localizados: start, download, queue, history, premium, account, settings, help, terms, privacy y paysupport.
- Botón del chat: **🌐 Open / Abrir**, destino `https://api.webrom.cc`.
- Main Mini App habilitada mediante BotFather; `getMe` confirmó `has_main_web_app=true`.
- Los comandos dirigen a la sección correspondiente de la Mini App y los botones incorporan idioma y vista.
- No se activaron permisos administrativos, ubicación, Secretary Mode, inline ni funciones de grupos que el servicio no implementa.

Los valores públicos verificados están en `bot-profile.json`. Para repetir la configuración: `node scripts/configure-bot.mjs`; usa credenciales del `.env` privado y no las imprime.

## Icono

Archivo fuente: `assets/branding/webrom-downloader-icon.png`. Versión JPEG para Telegram: `assets/branding/webrom-downloader-icon.jpg`.

Prompt utilizado con el generador integrado: “Use case: logo-brand. Asset type: finished square Telegram bot profile icon for Webrom Downloader, an advanced media downloader Mini App. A bold sculptural downward arrow integrated with a subtle W monogram and download tray, centered and recognizable at small sizes. Matte obsidian black background, silver chrome and white highlights. Neutral black, white and silver only; generous circular-crop padding; no words, badges, watermark or mockup frame.”

La Mini App incluye selector 🇪🇸/🇬🇧 y conserva la elección por dispositivo. El idioma visual no sustituye la validación de identidad firmada de Telegram.
