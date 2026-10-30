import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,writeFile,readFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {createHash} from 'node:crypto';
import {joinParts} from './join-parts.mjs';
const hash=b=>createHash('sha256').update(b).digest('hex');
test('reconstruye, verifica corrupción y evita sobrescrituras',async()=>{const dir=await mkdtemp(join(tmpdir(),'webrom-parts-'));try{const a=Buffer.from('primera parte'),b=Buffer.from('segunda parte');const m={version:1,original:'salida.bin',size:a.length+b.length,sha256:hash(Buffer.concat([a,b])),parts:[{name:'a.part00001',size:a.length,sha256:hash(a)},{name:'a.part00002',size:b.length,sha256:hash(b)}]};await writeFile(join(dir,m.parts[0].name),a);await writeFile(join(dir,m.parts[1].name),b);const path=join(dir,'test.manifest.json');await writeFile(path,JSON.stringify(m));const out=await joinParts(path);assert.deepEqual(await readFile(out),Buffer.concat([a,b]));await assert.rejects(joinParts(path),/ya existe/);m.original='otra.bin';m.parts[0].sha256='0'.repeat(64);await writeFile(path,JSON.stringify(m));await assert.rejects(joinParts(path),/SHA-256/);m.original='../escape';await writeFile(path,JSON.stringify(m));await assert.rejects(joinParts(path),/Nombre/)}finally{await rm(dir,{recursive:true,force:true})}});
