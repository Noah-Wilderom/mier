import { test as base, expect } from '@playwright/test'
import { desktopFixture, openFixture, root, headSHA } from './desktopFixture'
import type { CommandRun, CustomCommand } from '../src/repositoryToolTypes'
const test=base.extend<{desktop:Awaited<ReturnType<typeof desktopFixture>>}>({desktop:async({page},use)=>{const fixture=await desktopFixture(page);fixture.handlers.set('main.RepositoryService.Tags',()=>[]);await use(fixture);expect(fixture.unexpected).toEqual([]);expect(fixture.pageErrors).toEqual([])}})
test('custom commands save literal arguments, stream output, and cancel without closing their panel',async({desktop,page})=>{
 const commands:CustomCommand[]=[],job:CommandRun={id:'run-1',root,name:'Tests',status:'running',stdout:'Reading project files…',stderr:'',truncated:false,exitCode:-1,error:'',startedAt:'2026-10-15T12:00:00Z',finishedAt:''};let started=false,canceled=false
 desktop.handlers.set('main.CommandService.Commands',()=>commands)
 desktop.handlers.set('main.CommandService.Terminals',()=>[{id:'kitty',name:'Kitty',available:true}])
 desktop.handlers.set('main.CommandService.Latest',()=>started?job:null)
 desktop.handlers.set('main.CommandService.SaveCommand',args=>{expect(args[0]).toMatchObject({name:'Tests',executable:'go',arguments:['test','./...','$(literal) a b'],timeoutSeconds:120});const command={...args[0],id:'command-1'};commands.push(command);return command})
 desktop.handlers.set('main.CommandService.Start',args=>{expect(args).toEqual([root,'command-1',headSHA,'feature']);started=true;return job})
 desktop.handlers.set('main.CommandService.Run',()=>job)
 desktop.handlers.set('main.CommandService.Cancel',args=>{expect(args).toEqual(['run-1']);canceled=true;job.status='canceled';job.error='context canceled';job.finishedAt='2026-10-15T12:00:01Z'})
 await openFixture(desktop);await page.getByRole('button',{name:'Repository tools…'}).click();const dialog=page.getByRole('dialog',{name:'Repository tools'});await dialog.getByRole('button',{name:'Advanced',exact:true}).click();await dialog.getByLabel('Git tool').selectOption('commands');await dialog.getByRole('button',{name:'New custom command',exact:true}).click();await dialog.getByLabel('Command name').fill('Tests');await dialog.getByLabel('Executable',{exact:true}).fill('go');await dialog.getByLabel('Arguments (JSON array)').fill('["test", "./...", "$(literal) a b"]');await dialog.getByRole('button',{name:'Save custom command',exact:true}).click();expect(started).toBe(false);await dialog.getByRole('button',{name:'Run Tests',exact:true}).click();await expect(dialog.getByLabel('Command stdout')).toContainText('Reading project files…');await expect(dialog.getByRole('button',{name:'Run Tests',exact:true})).toBeDisabled();await dialog.getByRole('button',{name:'Cancel command',exact:true}).click();await expect.poll(()=>canceled).toBe(true);await expect(dialog.getByRole('button',{name:'Run Tests',exact:true})).toBeEnabled();await expect(dialog).toBeVisible()
})
