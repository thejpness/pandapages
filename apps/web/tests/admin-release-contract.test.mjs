import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const timestamp='2026-08-08T18:00:00Z'
const classic='11111111-1111-4111-8111-111111111111'
const growing='22222222-2222-4222-8222-222222222222'
async function loadAPI(){return(await loadTypeScript('../src/lib/api.ts',import.meta.url,(value)=>value.replaceAll('import.meta.env.VITE_API_BASE',"''"))).module}
function response(body,status=200){return new Response(JSON.stringify(body),{status,headers:{'Content-Type':'application/json'}})}
function release(){return{release:2,createdAt:timestamp,editions:[{editionKey:'classic',versionId:classic,version:4},{editionKey:'growing-readers',versionId:growing,version:7}]}}

test('release parser accepts canonical one-to-five subsets and rejects empty, duplicate and reordered membership',async()=>{
  const api=await loadAPI()
  assert.equal(api.parseAdminReleaseSummary({release:1,createdAt:timestamp,editions:[{editionKey:'growing-readers',versionId:growing,version:7}]}).editions.length,1)
  assert.deepEqual(api.parseAdminReleaseSummary(release()).editions.map((item)=>item.editionKey),['classic','growing-readers'])
  assert.throws(()=>api.parseAdminReleaseSummary({release:1,createdAt:timestamp,editions:[]}),/Invalid admin response/)
  assert.throws(()=>api.parseAdminReleaseSummary({release:1,createdAt:timestamp,editions:[{editionKey:'growing-readers',versionId:growing,version:7},{editionKey:'classic',versionId:classic,version:4}]}),/Invalid admin response/)
  assert.throws(()=>api.parseAdminReleaseSummary({release:1,createdAt:timestamp,editions:[{editionKey:'classic',versionId:classic,version:4},{editionKey:'classic',versionId:growing,version:7}]}),/Invalid admin response/)
})

test('release wrapper posts a partial manifest to the release endpoint and never uses legacy publish',async(t)=>{
  const originalFetch=globalThis.fetch;t.after(()=>{globalThis.fetch=originalFetch})
  let request
  globalThis.fetch=async(url,init)=>{request={url:String(url),init};return response({slug:'partial-story',outcome:'created',release:release()})}
  const api=await loadAPI()
  const result=await api.adminCreateRelease('partial-story',{editions:[{editionKey:'growing-readers',versionId:growing}]})
  assert.equal(result.release.release,2)
  assert.equal(request.url,'/api/v1/admin/stories/partial-story/releases')
  assert.equal(request.init.method,'POST')
  assert.doesNotMatch(request.url,/\/publish$/)
  assert.deepEqual(JSON.parse(String(request.init.body)),{editions:[{editionKey:'growing-readers',versionId:growing}]})
})
