import React, {useEffect, useRef, useState} from 'react';
import {createRoot} from 'react-dom/client';
import {BrowserRouter, Link, NavLink, Route, Routes, useLocation, useNavigationType} from 'react-router-dom';
import Playground from './Playground';
import FileView from './FileView';
import './style.css';
function Readonly({label,files}){const [active,setActive]=useState(files[0]?.path||'');return <FileView label={label} files={files} activePath={files.some(file=>file.path===active)?active:files[0]?.path} onSelect={setActive}/>;}
function Home(){return <section><p className="eyebrow">GO 生态 · 显式语义 · 普通 GO 输出</p><h1>写得更明确。<br/><em>仍然是 Go。</em></h1><p className="intro">把重复交给宏，把状态交给 enum。Goxide 为熟悉的 Go 加上有限、可检查的语言能力，生成你能阅读、审查和继续使用的 Go 源码。</p><div className="toolbar"><Link className="button primary" to="/playground.html">亲手试一段代码 →</Link><Link className="button" to="/rfc.html">了解语言设计 →</Link></div><p className="caption">语言名 Goxide · 命令 goxide · 源文件 .gox · 可执行子集，持续演进。</p><div className="language-topics"><div><h2>可审查的宏</h2><p>有限捕获、重复与嵌套，明确求值与名字绑定边界。</p><Link to="/playground.html?example=macro">看声明宏 →</Link></div><div><h2>enum 与 match</h2><p>闭合状态、穷尽检查、guard 与有限表达式组合。</p><Link to="/playground.html?example=enum">看状态匹配 →</Link></div><div><h2>缺失与失败</h2><p>Option/Result 与显式传播，Go 返回值的静态适配。</p><Link to="/playground.html?example=result">看错误传播 →</Link></div></div><h2>当前边界</h2><p>用户泛型函数已支持单关联协议约束；泛型协议、泛型 impl、多协议组合、完整 trait、用户过程宏与 LSP 尚未交付。部分 match / ? 组合仍有限。</p><p>路径只读不等于全局不可变，可写引用不承诺独占；这里不宣称完整 Rust 借用安全或零分配。</p><p>浏览器仅转译一个用户包与固定随附依赖，不加载任意模块、不执行 Go。当前能力以实际编译器清单与 RFC 摘要为准。</p></section>;}
function RFC({manifest}){
  const location=useLocation(), itemId=new URLSearchParams(location.search).get('feature')||manifest.catalog.items[0].id;
  const item=manifest.catalog.items.find(item=>item.id===itemId);
  return <section><p className="eyebrow">RFC 特性 · 当前编译器的公开摘要</p><h1>从设计到今天能做什么。</h1><p className="provenance">{manifest.commit} · 复核 {manifest.catalog.reviewedAt}</p><div className="rfc-browser"><nav id="rfc-list" aria-label="RFC 列表">{manifest.catalog.items.map(row=><Link key={row.id} to={'/rfc.html?feature='+encodeURIComponent(row.id)} aria-current={row.id===itemId?'page':undefined}>{row.rfcs.map(id=>'RFC '+id).join(' / ')} · {row.title}<small>{manifest.catalog.statusLabels[row.status]}</small></Link>)}</nav><article id="rfc-content">{!item?<p role="alert">找不到此 RFC 条目。</p>:<><h2>{item.title}</h2><p>{manifest.catalog.statusLabels[item.status]} · {{browser:'浏览器可转译',cli:'仅 CLI 工程',unavailable:'设计 / 开发中'}[item.support]}</p><p>{item.summary}</p><h3>设计与用法</h3><p>{item.usage}</p><p>边界：{item.limitation}</p>{item.development&&<p>开发快照：{item.development}</p>}{item.snippet&&<><h3>{item.snippet.language}</h3><Readonly key={item.id+'-snippet'} label="设计示例" files={[{path:'example',source:item.snippet.text}]} /><p>{item.snippet.note}</p></>}{item.examples.map(id=>{const example=manifest.examples.find(example=>example.id===id);return <div key={item.id+'-'+id}><h3>{example.title}</h3><Link to={'/playground.html?example='+id}>打开 Playground →</Link><Readonly label="RFC 示例" files={[{path:'playground.gox',source:example.source},{path:'playground.go',source:example.go}]} /><pre>CLI 运行输出（浏览器不执行）{'\n'+example.stdout}</pre></div>;})}{item.checks.map(id=>{const check=manifest.catalog.checks.find(check=>check.id===id);return <div key={item.id+'-'+id}><h3>拒绝例：{id}</h3><Readonly label="拒绝例" files={[{path:'rejected.gox',source:check.source},{path:'diagnostic.txt',source:check.diagnostic}]} /></div>;})}</>}</article></div><aside className="caption">{manifest.catalog.rules.map(rule=><p key={rule}>{rule}</p>)}</aside></section>;
}
function Local({manifest}){return <section><p className="eyebrow">CLI 工程 · 浏览器外运行</p><h1>从单文件走向工程。</h1><p>网站构建时由当前 CLI 转译并运行以下快照。它们不属于浏览器的可编辑输入。</p><p>新的完整 goxide.toml 示例等待项目配置合同定稿。既有配置只说明当前实现，不作为新 schema 承诺。构建脚本需要明确授权，本网站不会执行或授权它们。</p><div id="cli-example">{manifest.cliExamples.map(example=><div key={example.id}><h2>{example.title}</h2><Readonly label="CLI 源码与结果" files={example.files.flatMap(file=>[{path:'source/'+file.path,source:file.source},{path:'generated/'+file.path.replace(/\.gox$/,'.go'),source:file.go}])} /><pre>{example.stdout}</pre></div>)}</div><div id="project-examples">{manifest.projectExamples.map(project=><div key={project.id}><h2>{project.title}</h2>{project.note&&<p>{project.note}</p>}<Readonly label="工程快照" files={project.files} />{project.runs.map((run,index)=><div key={index}><p className="provenance">{run.command}</p><pre>{run.stdout}</pre></div>)}</div>)}</div></section>;}
function Shell({manifest}){
  const location=useLocation(), navigationType=useNavigationType(), routeScroll=useRef(new Map()), visible=location.pathname==='/playground.html';
  const [visited,setVisited]=useState(visible),main=useRef(null);
  useEffect(()=>{if(visible)setVisited(true);const title={'/':'语言介绍','/index.html':'语言介绍','/rfc.html':'RFC 特性','/playground.html':'Playground','/local.html':'本地使用'}[location.pathname]||'找不到页面';document.title=title+' · Goxide';main.current.focus({preventScroll:true});window.scrollTo(0,navigationType==='POP'?(routeScroll.current.get(location.key)||0):0);
    const record=()=>routeScroll.current.set(location.key,window.scrollY);window.addEventListener('scroll',record,{passive:true});return()=>window.removeEventListener('scroll',record);
  },[location.pathname,location.search,location.key,navigationType]);
  return <><a className="skip" href="#main">跳到主内容</a><header><Link className="brand" to="/">g <span>Goxide</span></Link><nav aria-label="主导航"><NavLink to="/rfc.html">RFC 特性</NavLink><NavLink to="/playground.html">Playground</NavLink><NavLink to="/local.html">本地使用</NavLink></nav></header><main id="main" tabIndex={-1} ref={main}>
    <Routes><Route path="/" element={<Home/>}/><Route path="/index.html" element={<Home/>}/><Route path="/rfc.html" element={<RFC manifest={manifest}/>}/><Route path="/local.html" element={<Local manifest={manifest}/>}/><Route path="/playground.html" element={null}/><Route path="*" element={<section><h1>找不到此页面。</h1></section>}/></Routes>
    {(visited||visible)&&<Playground manifest={manifest} visible={visible}/>}
  </main><footer><Link to="/">Goxide · goxide</Link><span>MPL-2.0 · <a href="/GO-LICENSE.txt">Go runtime license</a> · <a href="/THIRD-PARTY-LICENSES.txt">前端依赖许可</a></span></footer></>;
}
function App(){
  const [manifest,setManifest]=useState(null),[error,setError]=useState('');
  useEffect(()=>{const controller=new AbortController();(async()=>{
    const response=await fetch('/examples.json',{cache:'no-store',signal:controller.signal});if(!response.ok)throw new Error('示例清单 HTTP '+response.status);
    const data=await response.json(),context=data.dependencyContext;
    if(!/^[a-f0-9]{64}$/.test(data.wasmSha256||'')||!context?.source||!context.sha256)throw new Error('编译器清单缺少完整性信息');
    const hash=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',new TextEncoder().encode(context.source))),b=>b.toString(16).padStart(2,'0')).join('');
    if(hash!==context.sha256||data.catalog.compilerCommit!==data.commit)throw new Error('清单版本或项目依赖根不匹配');
    setManifest(data);
  })().catch(error=>{if(error.name!=='AbortError')setError(String(error));});return()=>controller.abort();},[]);
  return error?<main><h1>网站数据暂时不可用</h1><p role="alert">{error}</p></main>:manifest?<Shell manifest={manifest}/>:<main><p role="status">正在载入网站数据…</p></main>;
}
createRoot(document.getElementById('root')).render(<BrowserRouter><App/></BrowserRouter>);
