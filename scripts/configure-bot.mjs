import {readFile,writeFile} from 'node:fs/promises';
import {resolve} from 'node:path';
const env=Object.fromEntries((await readFile('.env','utf8')).split(/\r?\n/).filter(s=>/^[A-Z_]+=/.test(s)).map(s=>{const i=s.indexOf('=');return [s.slice(0,i),s.slice(i+1).trim()]}));
if(!env.BOT_TOKEN)throw new Error('BOT_TOKEN missing');
const base=env.TELEGRAM_API_URL+'/bot'+env.BOT_TOKEN+'/';
export const commandRows=[
 ['start','🚀 Abrir Mini App','🚀 Open Mini App'],['download','⬇️ Nueva descarga','⬇️ New download'],['queue','📚 Cola y progreso','📚 Queue and progress'],['history','🕘 Historial y archivos','🕘 History and files'],['premium','⭐ Planes y suscripción','⭐ Plans and subscription'],['account','👤 Mi cuenta y límites','👤 Account and limits'],['settings','⚙️ Ajustes de la app','⚙️ App settings'],['help','❓ Ayuda y soporte','❓ Help and support'],['terms','📃 Condiciones de uso','📃 Terms of use'],['privacy','🔒 Privacidad','🔒 Privacy policy'],['paysupport','💬 Pagos y reembolsos','💬 Payments and refunds']
];
async function call(method,payload){try{const r=await fetch(base+method,{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(payload),signal:AbortSignal.timeout(30000)});const data=await r.json();if(!data.ok)throw new Error(method+': '+data.description);return data.result}catch(e){if(e.message?.startsWith(method+':'))throw e;throw new Error(method+': Telegram connection failed')}}
const texts={
 '':{description:'🇪🇸 Descarga vídeos, música, galerías y archivos desde YouTube, Instagram, TikTok, Facebook, X y más. Elige calidad y formato, sigue la cola y recibe los archivos aquí. Premium: hasta 4K, recortes y archivos grandes divididos.\n\n🇬🇧 Download videos, music, galleries and files. Choose quality and format, track your queue and receive files here. Premium adds up to 4K, trimming and split large files.\n\n🚀 Open App / Abrir app · ⭐ Telegram Stars\nAvailability depends on the source.',short:'🇪🇸 Vídeos, música y archivos. 🇬🇧 Videos, music & files. ⬇️ Mini App · ⭐ Premium · by Webrom'},
 es:{description:'🇪🇸 Tu centro de descargas en Telegram.\n\n⬇️ Vídeos, música, galerías y archivos de YouTube, Instagram, TikTok, Facebook, X y más.\n🎛 Elige calidad y formato; consulta cola, progreso e historial.\n⭐ Premium: hasta 4K, FLAC, listas, subtítulos, recortes y archivos grandes divididos en partes.\n🌗 Temas Día/Noche y tamaño de letra ajustable.\n\n🚀 Pulsa Abrir app para empezar.\n⭐ Suscripción con Telegram Stars.\nLa disponibilidad depende del sitio; sin descifrar DRM.\n\n🇬🇧 English supported.',short:'🇪🇸 Vídeos, música, galerías y archivos en Telegram. ⬇️ Mini App · ⭐ Premium · 🇬🇧 English'},
 en:{description:'🇬🇧 Your download hub inside Telegram.\n\n⬇️ Save videos, music, galleries and files from YouTube, Instagram, TikTok, Facebook, X and more.\n🎛 Choose quality and format; track your queue, progress and history.\n⭐ Premium: up to 4K, FLAC, playlists, subtitles, trimming and split large files.\n🌗 Day/Night themes and adjustable text size.\n\n🚀 Tap Open App to get started.\n⭐ Subscribe with Telegram Stars.\nAvailability depends on each source; no DRM decryption.\n\n🇪🇸 Español disponible.',short:'🇬🇧 Videos, music, galleries & files in Telegram. ⬇️ Mini App · ⭐ Premium · 🇪🇸 Español'}
};
for(const [lang,t] of Object.entries(texts)){
 if([...t.description].length>512||[...t.short].length>120)throw new Error('Text length exceeds Telegram limit for '+lang);
 await call('setMyName',{name:'Webrom Downloader',language_code:lang});
 await call('setMyDescription',{description:t.description,language_code:lang});
 await call('setMyShortDescription',{short_description:t.short,language_code:lang});
 await call('setMyCommands',{language_code:lang,commands:commandRows.map(([command,es,en])=>({command,description:lang==='es'?'🇪🇸 '+es:lang==='en'?'🇬🇧 '+en:es+' / '+en}))});
}
await call('setChatMenuButton',{menu_button:{type:'web_app',text:'🌐 Open / Abrir',web_app:{url:env.PUBLIC_URL}}});
const form=new FormData();form.set('photo',JSON.stringify({type:'static',photo:'attach://icon'}));form.set('icon',new Blob([await readFile('assets/branding/webrom-downloader-icon.jpg')],{type:'image/jpeg'}),'icon.jpg');
const response=await fetch(base+'setMyProfilePhoto',{method:'POST',body:form,signal:AbortSignal.timeout(30000)});const photo=await response.json();if(!photo.ok)throw new Error('Profile photo: '+photo.description);
const verification={};for(const lang of ['', 'es','en']){verification[lang||'default']={description:await call('getMyDescription',{language_code:lang}),about:await call('getMyShortDescription',{language_code:lang}),commands:await call('getMyCommands',{language_code:lang})}};
await writeFile('docs/bot-profile.json',JSON.stringify({name:'Webrom Downloader',username:env.BOT_USERNAME,profilePhotoSet:true,menu:await call('getChatMenuButton',{}),languages:verification},null,2));
console.log('Verified: profile icon, name, descriptions, commands in es/en and bilingual fallback; Mini App menu.');
