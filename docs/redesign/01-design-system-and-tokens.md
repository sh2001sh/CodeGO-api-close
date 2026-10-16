# 设计系统与变量

## 设计权威与调整
继承 web/default/DESIGN.md 的暖白、铜橙、Space Grotesk、Public Sans、JetBrains Mono 和 Lucide。暖底来自现有品牌。与旧方案相比：正文由14px提高到16px；装饰性副文案移出产品界面；大圆角、重复阴影与毛玻璃收敛。首页超大字形来自现有品牌及用户明确方向。

## 色彩矩阵
| 语义 | 浅色 | 深色 | 用法 |
| --- | --- | --- | --- |
| canvas | #FEFDFB | #171411 | 页面底 |
| surface | #FFFFFF | #231F1B | 表单、数据、浮层 |
| ink | #221F1C | #FAF7F3 | 正文、标题 |
| secondary | #6F665E | #C2B7AC | 非关键元数据 |
| brand | #B8562E | #F3A27F | 主操作、选中 |
| brand-hover | #A84F2D | #FFC0A2 | 主操作悬停 |
| decorative | #D26739 | #F3A27F | 装饰路线，浅底正文禁用 |
| border | #D8D1CA | #75685C | 非关键分隔；表单还须文字或轮廓明确 |
| focus | #A84F2D | #FFC0A2 | 2px焦点及2px偏移 |
| success | #17603A | #8AD7AD | 图标与“成功” |
| warning | #7B4B00 | #F1C36F | 图标与“待处理” |
| danger | #A72727 | #FFAAA4 | 图标与具体失败 |

正文对比下限4.5:1，大文字和必要图形3:1。主按钮浅色铜底白字，深色亮铜底深字；禁止深色亮铜按钮仍用白字。细分隔线不承担唯一状态表达。

## 字体与字阶
| 用途 | 家族 | 字号 | 行高 | 约束 |
| --- | --- | --- | --- | --- |
| 首页品牌 | Space Grotesk Variable | clamp(96px,16vw,224px) | 0.9 | 只用于品牌，不套表单 |
| 首页中文标题 | 中文系统字体 | clamp(32px,4.4vw,64px) | 1.16 | 完整短句，最多两行 |
| 页面标题 | Public Sans及中文后备 | 32px；窄屏24px | 1.25 | 不加解释副标题 |
| 区块标题 | 同正文 | 24px | 1.35 | 上间距大于下间距 |
| 正文及表格 | Public Sans及中文后备 | 16px | 1.6 | 保持清晰和缩放 |
| 指标 | Space Grotesk | clamp(24px,3vw,48px) | 1.2 | 等宽数字，绑定单位 |
| 代码 | JetBrains Mono | 16px | 1.6 | 局部横滚或受控换行 |

中文后备：PingFang SC、HarmonyOS Sans SC、MiSans、Microsoft YaHei、Noto Sans SC、sans-serif。正文桌面65–75字符，移动35–60字符。代码文件名、模型 ID、流水号不省略唯一识别末尾；详情允许全量复制。

## 间距、形状、层级
基础单位4px；刻度4/8/12/16/24/32/40/48/64/96。按钮44或48px高；图标20或24px；触控区至少44px且相邻8px。圆角0用于数据分割，4用于主按钮，8用于输入与菜单，12用于确需独立表面的容器。胶囊仅用于短状态和筛选。

公开容器最大1184px；控制台内容最大1440px，阅读最大760px。普通区块不加阴影；菜单使用0 8px 24px rgba(34,31,28,.12)，对话框使用0 16px 48px rgba(34,31,28,.18)。层级：正文0、粘性导航20、菜单30、抽屉40、模态50、通知60。

## 可编辑变量
```css
:root {
  --canvas:#fefdfb; --surface:#fff; --ink:#221f1c;
  --muted:#6f665e; --brand:#b8562e; --brand-hover:#a84f2d;
  --line:#d8d1ca; --focus:#a84f2d;
  --space:4px; --control:48px; --radius:8px;
  --duration-feedback:180ms; --duration-enter:280ms;
  --duration-exit:180ms; --stagger:40ms;
  --ease-enter:cubic-bezier(.16,1,.3,1);
  --ease-exit:cubic-bezier(.4,0,1,1);
}
```
真实产品在已有语义变量上对应映射，避免新增第二套互相覆盖的品牌色。demo独立集中变量，不修改现有系统。

## 结构规则
应用 OD-LAYOUT-PRIMITIVES：数值与说明用 od-stat；标签与字段用 od-field；操作行用 od-row；数据截断用 od-truncate，完整值通过详情可达；筛选条用 od-rail；条带和滚动用 od-screen。标题及按钮为作者文案，改短或改布局，不截断。

图片保留完整 frame；真实logo保持viewBox比例，不放进会扭曲比例的固定双轴框。demo品牌SVG为32×32。所有图片来源与授权写入素材表。

## 全局状态
每个控件含默认、hover、focus、按压、selected、loading、disabled、error、success中实际适用状态。hover仅在支持悬停设备生效；焦点与hover并存，不以变浅文字表示hover。禁用提供可理解的业务状态，不只降低透明度。

## 全局运动契约
进入280ms，退出180ms；普通反馈180ms；列表错峰40ms且最多首6项，长列表整体交叉淡入180ms。页面切换不等待动画才加载。任何文字、价格和账务金额不从零滚动。动效ID、每页调用和组件差异见第11章及覆盖表。
