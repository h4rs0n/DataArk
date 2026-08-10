package assessmenteval

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const BlindPassDelay = 72 * time.Hour

type labelPageItem struct {
	SampleID       string `json:"sampleId"`
	Title          string `json:"title"`
	BodyText       string `json:"bodyText"`
	Language       string `json:"language"`
	BodyCharacters int    `json:"bodyCharacters"`
}

type labelPageData struct {
	Version        string          `json:"version"`
	ManifestDigest string          `json:"manifestDigest"`
	Pass           int             `json:"pass"`
	Items          []labelPageItem `json:"items"`
}

func BuildPassOneHTML(manifest Manifest) ([]byte, error) {
	if len(manifest.Items) != GoldSampleCount {
		return nil, fmt.Errorf("pass one requires %d manifest items, got %d", GoldSampleCount, len(manifest.Items))
	}
	return buildLabelHTML(manifest, manifest.Items, 1)
}

func BuildPassTwoHTML(manifest Manifest, passOne LabelSet, now time.Time) ([]byte, []string, error) {
	if err := ValidateArtifactLink(manifest, passOne.ManifestDigest); err != nil {
		return nil, nil, err
	}
	if passOne.Pass != 1 || passOne.CompletedAt.IsZero() {
		return nil, nil, errors.New("pass one must be complete before creating pass two")
	}
	if now.Before(passOne.CompletedAt.Add(BlindPassDelay)) {
		remaining := passOne.CompletedAt.Add(BlindPassDelay).Sub(now).Round(time.Minute)
		return nil, nil, fmt.Errorf("pass two remains blind for %s", remaining)
	}
	core := make([]ManifestItem, 0, CoreSampleCount)
	stress := make([]ManifestItem, 0, StressSampleCount)
	for _, item := range manifest.Items {
		switch {
		case strings.HasPrefix(item.Stratum, "core:"):
			core = append(core, item)
		case strings.HasPrefix(item.Stratum, "stress:"):
			stress = append(stress, item)
		}
	}
	sortManifestItems(core, manifest.Seed, "pass-two-core")
	sortManifestItems(stress, manifest.Seed, "pass-two-stress")
	if len(core) < 20 || len(stress) < 10 {
		return nil, nil, errors.New("manifest does not contain enough core and stress samples for pass two")
	}
	selected := append(append([]ManifestItem{}, core[:20]...), stress[:10]...)
	sortManifestItems(selected, manifest.Seed, "pass-two-order")
	payload, err := buildLabelHTML(manifest, selected, 2)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(selected))
	for _, item := range selected {
		ids = append(ids, item.SampleID)
	}
	return payload, ids, nil
}

func BuildAdjudicationHTML(manifest Manifest, passOne, passTwo LabelSet) ([]byte, []string, error) {
	if err := ValidateArtifactLink(manifest, passOne.ManifestDigest); err != nil {
		return nil, nil, err
	}
	if err := ValidateArtifactLink(manifest, passTwo.ManifestDigest); err != nil {
		return nil, nil, err
	}
	first, err := indexLabels(passOne.Labels)
	if err != nil {
		return nil, nil, err
	}
	second, err := indexLabels(passTwo.Labels)
	if err != nil {
		return nil, nil, err
	}
	selected := make([]ManifestItem, 0)
	ids := make([]string, 0)
	for _, item := range manifest.Items {
		left, leftOK := first[item.SampleID]
		right, rightOK := second[item.SampleID]
		if leftOK && rightOK && !left.Unjudgeable && !right.Unjudgeable && scoresConflict(left.Scores, right.Scores) {
			selected = append(selected, item)
			ids = append(ids, item.SampleID)
		}
	}
	if len(selected) == 0 {
		return nil, nil, errors.New("no pass-one/pass-two conflicts require adjudication")
	}
	payload, err := buildLabelHTML(manifest, selected, 3)
	return payload, ids, err
}

func buildLabelHTML(manifest Manifest, items []ManifestItem, pass int) ([]byte, error) {
	page := labelPageData{Version: LabelVersion, ManifestDigest: manifest.Digest, Pass: pass, Items: make([]labelPageItem, 0, len(items))}
	for _, item := range items {
		page.Items = append(page.Items, labelPageItem{SampleID: item.SampleID, Title: item.Title, BodyText: item.BodyText, Language: item.Language, BodyCharacters: item.BodyCharacters})
	}
	payload, err := json.Marshal(page)
	if err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(payload)
	html := strings.Replace(labelHTMLTemplate, "__DATA__", encoded, 1)
	return []byte(html), nil
}

func sortManifestItems(items []ManifestItem, seed, purpose string) {
	sort.Slice(items, func(left, right int) bool {
		leftDigest := sha256.Sum256([]byte(seed + "\x00" + purpose + "\x00" + items[left].SampleID))
		rightDigest := sha256.Sum256([]byte(seed + "\x00" + purpose + "\x00" + items[right].SampleID))
		return hex.EncodeToString(leftDigest[:]) < hex.EncodeToString(rightDigest[:])
	})
}

const labelHTMLTemplate = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>DataArk Article Assessment Blind Labelling</title>
<style>
body{margin:0;background:#f4f1e8;color:#20231f;font:16px/1.65 system-ui,-apple-system,sans-serif}main{max-width:1040px;margin:auto;padding:22px}.bar,.card,.rubric{background:#fff;border:1px solid #d7d1c3;border-radius:10px;padding:18px;margin-bottom:16px}.bar{position:sticky;top:0;z-index:2;display:flex;gap:12px;align-items:center}.bar strong{margin-right:auto}.article{white-space:pre-wrap;font-family:ui-serif,Georgia,serif;font-size:18px;max-height:54vh;overflow:auto;border-top:1px solid #ddd;padding-top:16px}.grid{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}.field label{display:block;font-weight:650}.field input[type=number],textarea,select{width:100%;box-sizing:border-box;padding:9px;border:1px solid #aaa;border-radius:6px;background:#fff}textarea{min-height:76px}.checks{display:flex;gap:24px;margin:14px 0}button{padding:9px 15px;border:0;border-radius:6px;background:#315845;color:#fff;cursor:pointer}button.secondary{background:#6a706a}button:disabled{opacity:.45}.muted{color:#68706a;font-size:14px}.rubric summary{cursor:pointer;font-weight:700}.rubric table{width:100%;border-collapse:collapse}.rubric td,.rubric th{padding:5px;border-bottom:1px solid #ddd;text-align:left}@media(max-width:700px){.grid{grid-template-columns:1fr}.bar{flex-wrap:wrap}}
</style></head><body><main>
<div class="bar"><strong id="progress"></strong><button class="secondary" id="prev">上一篇</button><button id="next">保存并下一篇</button><button id="export">导出标注 JSON</button></div>
<details class="rubric"><summary>评分锚点（评估文章本身，不考虑站点、作者声誉、热度和新旧）</summary><table><tr><th>分数</th><th>含义</th></tr><tr><td>0–19</td><td>几乎没有有效阅读价值</td></tr><tr><td>20–39</td><td>较弱，信息或论证明显不足</td></tr><tr><td>40–59</td><td>普通，能够提供一些有效信息</td></tr><tr><td>60–74</td><td>良好，有明确、具体且完整的价值</td></tr><tr><td>75–89</td><td>优秀，有深入、可复用、充分支持的内容</td></tr><tr><td>90–100</td><td>极少数卓越文章；必须具体、原创、可复用且证据充分</td></tr></table><p>Quality：总体阅读收获。Depth：机制、因果、权衡、限制、反例或实验的分析深度。Evergreen：脱离即时新闻、版本或动态后仍可复用的程度。</p></details>
<section class="card"><h1 id="title"></h1><div class="muted" id="meta"></div><article class="article" id="body"></article></section>
<section class="card"><div class="grid">
<div class="field"><label for="quality">Quality 0–100</label><input id="quality" type="number" min="0" max="100" step="1"></div>
<div class="field"><label for="depth">Depth 0–100</label><input id="depth" type="number" min="0" max="100" step="1"></div>
<div class="field"><label for="evergreen">Evergreen 0–100</label><input id="evergreen" type="number" min="0" max="100" step="1"></div>
</div><div class="field"><label for="reason">简短人工理由</label><textarea id="reason" maxlength="300"></textarea></div>
<div class="field"><label for="genre">文章类型</label><select id="genre"><option value="">请选择</option><option>analysis</option><option>tutorial</option><option>reference</option><option>essay</option><option>news</option><option>release</option><option>personal-update</option><option>other</option></select></div>
<div class="checks"><label><input id="extractionBad" type="checkbox"> 正文提取有明显问题</label><label><input id="unjudgeable" type="checkbox"> 无法判断</label></div></section>
</main><script>
const page=JSON.parse(new TextDecoder().decode(Uint8Array.from(atob('__DATA__'),c=>c.charCodeAt(0))));
const key='dataark-assessment:'+page.manifestDigest+':pass-'+page.pass;
let state=JSON.parse(localStorage.getItem(key)||'null')||{startedAt:new Date().toISOString(),labels:{},index:0}; let openedAt=Date.now();
const el=id=>document.getElementById(id), fields=['quality','depth','evergreen','reason','genre','extractionBad','unjudgeable'];
function current(){return page.items[state.index]}
function save(){const item=current(), old=state.labels[item.sampleId]||{durationSeconds:0}; old.durationSeconds+=(Date.now()-openedAt)/1000; openedAt=Date.now(); for(const name of fields){old[name]=(name==='extractionBad'||name==='unjudgeable')?el(name).checked:el(name).value} state.labels[item.sampleId]=old;localStorage.setItem(key,JSON.stringify(state))}
function render(){const item=current(),label=state.labels[item.sampleId]||{};el('title').textContent=item.title||'(无标题)';el('body').textContent=item.bodyText;el('meta').textContent='语言 '+item.language+' · 正文 '+item.bodyCharacters+' 字符 · 样本 '+item.sampleId;for(const name of fields){if(name==='extractionBad'||name==='unjudgeable')el(name).checked=!!label[name];else el(name).value=label[name]??''}el('progress').textContent='盲标第 '+page.pass+' 轮 · '+(state.index+1)+' / '+page.items.length+' · 已保存 '+Object.keys(state.labels).length;el('prev').disabled=state.index===0;el('next').textContent=state.index===page.items.length-1?'保存':'保存并下一篇';window.scrollTo(0,0);openedAt=Date.now()}
function score(name){const value=Number(el(name).value);return Number.isInteger(value)&&value>=0&&value<=100?value:null}
function valid(){if(el('unjudgeable').checked)return true;return score('quality')!==null&&score('depth')!==null&&score('evergreen')!==null&&el('reason').value.trim()&&el('genre').value}
el('prev').onclick=()=>{save();state.index=Math.max(0,state.index-1);render()};el('next').onclick=()=>{if(!valid()){alert('请填写三个整数分数、理由和文章类型，或标记为无法判断。');return}save();if(state.index<page.items.length-1)state.index++;render()};
el('export').onclick=()=>{save();const complete=x=>x&&['quality','depth','evergreen'].every(name=>Number.isInteger(Number(x[name]))&&Number(x[name])>=0&&Number(x[name])<=100)&&x.reason&&x.genre;const missing=page.items.filter(item=>!state.labels[item.sampleId]||(!state.labels[item.sampleId].unjudgeable&&!complete(state.labels[item.sampleId])));if(missing.length){alert('仍有 '+missing.length+' 篇未完整标注。');return}const labels=page.items.map(item=>{const x=state.labels[item.sampleId];return{sampleId:item.sampleId,scores:{quality:Number(x.quality)||0,depth:Number(x.depth)||0,evergreen:Number(x.evergreen)||0},reason:String(x.reason||'').trim(),genre:x.genre||'',extractionBad:!!x.extractionBad,unjudgeable:!!x.unjudgeable,durationSeconds:Math.round(x.durationSeconds||0)}});const output={version:page.version,manifestDigest:page.manifestDigest,pass:page.pass,startedAt:state.startedAt,completedAt:new Date().toISOString(),labels};const blob=new Blob([JSON.stringify(output,null,2)+'\n'],{type:'application/json'}),a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download='article-assessment-pass-'+page.pass+'.json';a.click();setTimeout(()=>URL.revokeObjectURL(a.href),1000)};
render();
</script></body></html>`
