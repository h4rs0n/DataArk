# Frontend 易错点

本文记录 DataArk 前端开发中已经踩过、容易复发的问题。修改 UI 前应先查组件库真实 API，不要凭相似组件库经验写参数。

## Arco Alert 不支持 `message`

DataArk 使用的是 `@arco-design/web-vue`，不是 Ant Design Vue。Arco 的 `<a-alert>` 文本参数是 `title`，正文内容使用默认插槽；它没有 `message` prop。

错误写法：

    <a-alert type="success" :message="messageText" show-icon />

结果：

    页面会渲染一个带图标和样式的 Alert 容器，但提示文字为空。浏览器不一定报 Vue 编译错误，所以这个问题很容易在只看接口成功时漏掉。

正确写法：

    <a-alert type="success" :title="messageText" show-icon />

如果需要标题和正文：

    <a-alert type="warning" title="发现数据不一致" show-icon>
      部分 HTML 文件还没有写入搜索索引，可以点击修复。
    </a-alert>

## 验证要求

涉及 Arco 组件参数时，至少做以下检查：

- 查 `web/node_modules/@arco-design/web-vue/es/<component>/<component>.d.ts` 或官方文档确认 prop 名称。
- 在浏览器里看 DOM snapshot，确认提示文字真的出现在可访问性树中。
- 检查 console。没有 console error 不代表参数写对了。
- 对成功、失败、空数据三种状态分别看一眼，不要只验证接口返回 200。

## 本次教训

`/consistency` 页面曾把成功提示写成 `:message="report.consistent ? '三方数据一致' : '发现数据不一致'"`。真实 Docker 页面中 Alert 容器出现了，但没有文字。原因就是 Arco Alert 忽略了不存在的 `message` prop。

修复时应同时检查同类用法，例如：

    rg "a-alert.*message|:message=" web/src
