import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

const keys=['classic','confident-readers','growing-readers','story-explorers','little-listeners']
const ids=['11111111-1111-4111-8111-111111111111','22222222-2222-4222-8222-222222222222','33333333-3333-4333-8333-333333333333','44444444-4444-4444-8444-444444444444','55555555-5555-4555-8555-555555555555']
const timestamp='2026-08-08T18:00:00Z'

async function loadRelease(){
  return (await loadTypeScript('../src/lib/story-release.ts',import.meta.url)).module
}
function version(editionKey,index,{draft=true,published=false}={}){
  return {editionKey,versionId:ids[index],version:index+1,createdAt:timestamp,isDraft:draft,isPublished:published,segmentCount:2,wordCount:6,chapterCount:0,health:'ready'}
}
function story(currentRelease=null,releases=[]){
  const editions=keys.map((editionKey,index)=>{
    const present=index<3
    const item=present?version(editionKey,index):null
    return {editionKey,status:present?'draft_only':'empty',publishedVersion:null,draftVersion:item?{versionId:item.versionId,version:item.version}:null,versionCount:item?1:0,updatedAt:present?timestamp:null,versions:item?[item]:[]}
  })
  return {slug:'release-story',title:'Release Story',author:null,language:'en-GB',rights:{},sourceUrl:null,status:currentRelease?'published_with_draft':'draft_only',publishedVersion:currentRelease?{versionId:currentRelease.editions[0].versionId,version:currentRelease.editions[0].version}:null,draftVersion:{versionId:ids[0],version:1},versionCount:1,createdAt:timestamp,updatedAt:timestamp,source:{status:'missing',currentVersion:null,versionCount:0,updatedAt:null},editions,currentRelease,releaseCount:releases.length,releases}
}

test('first release defaults to every authored healthy edition, not all five canonical slots',async()=>{
  const release=await loadRelease()
  const candidate=release.buildStoryReleaseCandidate(story())
  assert.deepEqual(candidate.filter((row)=>row.included).map((row)=>row.editionKey),keys.slice(0,3))
  assert.deepEqual(release.releaseCandidateRequest(candidate).map((item)=>item.editionKey),keys.slice(0,3))
})

test('later release preserves the current inclusion set while preferring new drafts',async()=>{
  const release=await loadRelease()
  const current={release:1,createdAt:timestamp,editions:[{editionKey:'classic',versionId:ids[0],version:1},{editionKey:'growing-readers',versionId:ids[2],version:3}]}
  const input=story(current,[current])
  const candidate=release.buildStoryReleaseCandidate(input)
  assert.deepEqual(candidate.filter((row)=>row.included).map((row)=>row.editionKey),['classic','growing-readers'])
  assert.equal(candidate.find((row)=>row.editionKey==='confident-readers').included,false)
  assert.equal(release.releaseCandidateMatchesCurrent(release.releaseCandidateRequest(candidate),current),true)
})

test('one-edition non-Classic release is a valid candidate shape',async()=>{
  const release=await loadRelease()
  const rows=release.buildStoryReleaseCandidate(story()).map((row)=>({...row,included:row.editionKey==='growing-readers'}))
  const request=release.releaseCandidateRequest(rows)
  assert.deepEqual(request,[{editionKey:'growing-readers',versionId:ids[2]}])
})
