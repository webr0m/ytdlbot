// Owner-only local verification: retrieve Telegram-confirmed parts and check
// complete reassembly. Credentials and Telegram file paths are never printed.
import {readFile,mkdir,stat,writeFile} from 'node:fs/promises';
import {createReadStream} from 'node:fs';
import {createHash} from 'node:crypto';
import {basename,resolve,join} from 'node:path';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {joinParts} from './join-parts.mjs';
const run=promisify(execFile);
const env=Object.fromEntries((await readFile('.env','utf8')).split(/\r?\n/).filter(s=>/^[A-Z_]+=/.test(s)).map(s=>{const i=s.indexOf('=');return [s.slice(0,i),s.slice(i+1).trim()]}));
const id=process.argv[2];if(!/^[a-f0-9]{32}$/.test(id||''))throw new Error('Usage: node scripts/verify-telegram-job.mjs JOB_ID');
async function api(method,payload){let result;try{const r=await fetch(env.TELEGRAM_API_URL+'/bot'+env.BOT_TOKEN+'/'+method,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload),signal:AbortSignal.timeout(300000)});result=await r.json()}catch{throw new Error('Telegram verification connection failed')};if(!result.ok)throw new Error('Telegram verification request failed');return result.result}
const response=await fetch('http://127.0.0.1:8095/api/jobs',{headers:{Authorization:'Bearer '+env.LOCAL_ADMIN_KEY}});if(!response.ok)throw new Error('Local admin access required');const jobs=await response.json();const job=jobs.find(j=>j.id===id);if(!job||job.state!=='done')throw new Error('The job must be completed');
const dir=resolve('data','verified-telegram',id);await mkdir(dir,{recursive:true});
async function retrieve(file){if(file.name!==basename(file.name)||/[\\/:]/.test(file.name))throw new Error('Unsafe filename');const path=join(dir,file.name);try{if((await stat(path)).size===file.size)return path}catch{}const remote=await api('getFile',{file_id:file.fileId});try{await run('docker',['cp','webrom-downloader-telegram-api-1:'+remote.file_path,path],{maxBuffer:1024*1024})}catch{throw new Error('Cannot copy verification file from local Telegram API')};if((await stat(path)).size!==file.size)throw new Error('Retrieved file size mismatch');return path}
const manifestFile=job.files.find(f=>f.name.endsWith('.manifest.json'));if(!manifestFile)throw new Error('No final manifest');const manifestPath=await retrieve(manifestFile);const manifest=JSON.parse(await readFile(manifestPath,'utf8'));let count=0;
let nextPart=0;await Promise.all([0,1].map(async()=>{while(nextPart<manifest.parts.length){const part=manifest.parts[nextPart++];const file=job.files.find(f=>f.name===part.name);if(!file)throw new Error('Manifest part missing from delivered files');await retrieve(file);console.log('Retrieved part '+(++count)+' / '+manifest.parts.length+' ('+(file.size/1e6).toFixed(1)+' MB)')}}));
let output=join(dir,manifest.original);try{await stat(output)}catch{output=await joinParts(manifestPath,undefined,p=>console.log('Verified reconstruction part '+p.part+' / '+p.total))}const hash=createHash('sha256');for await(const block of createReadStream(output,{highWaterMark:4*1024*1024}))hash.update(block);const actual=hash.digest('hex');if(actual!==manifest.sha256)throw new Error('Full reconstructed SHA-256 mismatch');
const report={job:id,parts:count,size:manifest.size,sha256:actual,manifestMatches:true,telegramReassemblyVerified:true,output};await writeFile(join(dir,'verification.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));
