import {createReadStream, createWriteStream} from 'node:fs';
import {readFile, stat, rm, link} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {basename, dirname, resolve} from 'node:path';
import {once} from 'node:events';
import {finished} from 'node:stream/promises';
import {fileURLToPath} from 'node:url';

export async function joinParts(manifestFile,outputDirectory,onProgress){
 const manifest=JSON.parse(await readFile(manifestFile,'utf8'));
 if(manifest.version!==1||!Array.isArray(manifest.parts)||!manifest.parts.length||manifest.parts.length>100000||!Number.isSafeInteger(manifest.size)||manifest.size<=0||!/^[a-f0-9]{64}$/.test(manifest.sha256))throw new Error('Manifiesto inválido');
 const safeName=name=>typeof name==='string'&&name.length>0&&name===basename(name)&&!/[\\/:\x00-\x1f]/.test(name)&&name!=='.'&&name!=='..';
 if(!safeName(manifest.original))throw new Error('Nombre de salida inválido');
 const dir=dirname(resolve(manifestFile));const output=resolve(outputDirectory||dir,manifest.original);const temporary=output+'.joining';
 try{await stat(output);throw new Error('La salida ya existe; no se sobrescribe')}catch(e){if(e.code!=='ENOENT')throw e}
 const seen=new Set();let size=0;
 for(const part of manifest.parts){if(!safeName(part.name)||seen.has(part.name)||!Number.isSafeInteger(part.size)||part.size<=0||!/^[a-f0-9]{64}$/.test(part.sha256))throw new Error('Parte inválida');seen.add(part.name);const info=await stat(resolve(dir,part.name));if(!info.isFile()||info.size!==part.size)throw new Error('Parte ausente o tamaño incorrecto: '+part.name);size+=part.size}
 if(size!==manifest.size)throw new Error('La suma de partes no coincide');
 const out=createWriteStream(temporary,{flags:'wx',highWaterMark:4*1024*1024});let streamError;out.on('error',e=>{streamError=e});const totalHash=createHash('sha256');
 try{await once(out,'open');for(const part of manifest.parts){const hash=createHash('sha256');for await(const block of createReadStream(resolve(dir,part.name),{highWaterMark:4*1024*1024})){if(streamError)throw streamError;hash.update(block);totalHash.update(block);if(!out.write(block))await once(out,'drain')}if(hash.digest('hex')!==part.sha256)throw new Error('SHA-256 incorrecto: '+part.name);onProgress?.({part:manifest.parts.indexOf(part)+1,total:manifest.parts.length,name:part.name})}if(totalHash.digest('hex')!==manifest.sha256)throw new Error('SHA-256 del archivo original incorrecto');out.end();await finished(out);await link(temporary,output);await rm(temporary);return output}catch(e){out.destroy();await finished(out).catch(()=>{});await rm(temporary,{force:true});throw e}
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){if(!process.argv[2]){console.error('Uso: node scripts/join-parts.mjs archivo.manifest.json [carpeta_salida]');process.exitCode=1}else{joinParts(resolve(process.argv[2]),process.argv[3],p=>console.log('Parte verificada '+p.part+' / '+p.total)).then(path=>console.log('Archivo reconstruido y verificado: '+path)).catch(e=>{console.error(e.message);process.exitCode=1})}}
