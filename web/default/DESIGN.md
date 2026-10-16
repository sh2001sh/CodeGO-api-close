---
name: CodeGo — Solaris Optics
description: 瓷白畫布、黑曜石操作面與日光橙訊號構成的模型接入、路由市場及控制台。
colors:
  canvas: "#FAF8F5"
  subtle: "#F0F2F5"
  card: "#FFFFFF"
  glass: "rgb(255 255 255 / 85%)"
  obsidian: "#0D0D10"
  brand-orange: "#EA580C"
  brand-orange-deep: "#C2410C"
  brand-orange-ink: "#9A3412"
  ink: "#0D0D10"
  ink-secondary: "#55555E"
  ink-inverse: "#FAF8F5"
  hairline: "rgb(13 13 16 / 6.5%)"
  emerald: "#087F5B"
  amber: "#946000"
  rose: "#BE234B"
  indigo: "#5146C8"
  dark-canvas: "#0D0D10"
  dark-subtle: "#141418"
  dark-ink: "#FAF8F5"
  dark-brand-ink: "#FDBA74"
typography:
  display: { fontFamily: "'Space Grotesk Variable', 'Public Sans Variable', sans-serif", fontWeight: 500, letterSpacing: "-0.04em" }
  headline: { fontSize: "32px", fontWeight: 600, lineHeight: 1.25 }
  title: { fontSize: "24px", fontWeight: 600, lineHeight: 1.35 }
  body: { fontFamily: "-apple-system, BlinkMacSystemFont, 'SF Pro Text', 'Inter Variable', 'Inter', 'Segoe UI', system-ui, 'PingFang SC', 'Hiragino Sans GB', 'HarmonyOS Sans SC', 'MiSans', 'Microsoft YaHei', 'Noto Sans SC', sans-serif", fontSize: "16px", lineHeight: 1.6 }
  metadata: { fontSize: "14px" }
  code: { fontFamily: "'JetBrains Mono Variable', ui-monospace, Consolas, monospace", fontSize: "16px", lineHeight: 1.75 }
rounded:
  panel: "20px"
  wallet-panel: "16px"
  control: "12px"
  compact: "8px"
  pill: "9999px"
spacing: { tight: "8px", control: "12px", base: "16px", group: "24px", panel: "32px", section: "40px" }
components:
  button-primary: { backgroundColor: "{colors.obsidian}", textColor: "{colors.card}", rounded: "{rounded.control}", height: "44px" }
  input: { rounded: "{rounded.control}", height: "44px", padding: "8px 14px" }
  card: { backgroundColor: "{colors.card}", rounded: "{rounded.panel}" }
  wallet-panel: { backgroundColor: "{colors.card}", rounded: "{rounded.wallet-panel}" }
  navigation-active: { backgroundColor: "{colors.obsidian}", textColor: "{colors.ink-inverse}", rounded: "{rounded.pill}" }
---

# CodeGo · Solaris Optics

## Overview

**Creative North Star: "Solaris Optics"**

瓷白畫布、黑曜石操作面與日光橙訊號構成橙白視覺世界，光學精確幾何承接品牌識別。工作介面以表面、留白、字重和數字建立層次，讓操作與真實資料直接可見。

**Key Characteristics:**
- 暖白主畫布、黑曜石主操作與橙色品牌訊號。
- 以表面、留白與字重分區，功能圖示統一 Lucide。
- 共用面板與錢包局部面板保留各自的圓角尺度。
- 明暗主題、真實交易狀態、可見焦點與減少動態。

### 來源與驗證邊界

本檔合併既有 Solaris 視覺契約與目前原始碼。基礎變數以主題檔為真源，局部元件以其最後生效樣式為準；局部裝飾值與尚未驗證的可達性結果不升格為全站規範。
核心來源：[主題](src/styles/theme.css)、[共用樣式](src/styles/index.css)、[Dawn 相容層](src/styles/dawn.css)、[共用元件](src/components/ui)。
頁面來源：[首頁](src/styles/market-home.css)、[市場](src/features/dawn/market/market.css)、[帳務](src/features/wallet/commerce.css)、[盲盒工作區](src/features/wallet/components/blind-box-workspace.css)、[錢包概覽](src/features/wallet/components/wallet-account-overview.tsx)、[文件](src/features/docs/styles.css)。
本次同步依據原始碼，未重新執行瀏覽器、對比或功能檢測；設計文件不承載整合驗收結論。
整合後桌面／手機、明暗主題、真實登入與錯誤流程的瀏覽器驗證由主工作線另記；此處不宣稱已全部通過。
測試夾具只能驗證被注入資料的版面與狀態，不證明真實帳戶、付款、後端結算、延遲或服務可用率。

## Colors

瓷白畫布提供安靜的工作底色，凹面淺灰建立層次，黑曜石承接集中操作，日光橙標示品牌與指定動作。
`--bg-canvas`、`--bg-subtle`、`--surface-card`、`--surface-glass`、`--surface-obsidian` 是跨頁表面契約；禁止再引入獨立舊銅橙配色。
卡片使用實白；玻璃白色透明度為 85%，模糊為 24px；光線邊界使用 `--hairline-light`。深色與頁面作用域依真源覆寫。
品牌橙及其深階、文字階對應前置資料；`--gradient-solaris` 直接重用主題中的橙色漸變，供品牌光線及指定裝飾使用。
主要操作與選取控制統一使用 `--primary`／`--primary-foreground`：淺色為黑曜石底與白字，深色為白底與黑曜石字；品牌橙保留於標識、光線及品牌點綴。
主要操作的懸停與按下使用 `--primary-hover`／`--primary-active`；市場入池、路由池、模型彈窗與獨立詳情不再覆蓋橙色漸變。共用元件的 brand 變體與錢包兌換操作保留指定橙色用途。次操作保留中性底、邊線與墨色文字。
`--ink-primary`、`--ink-secondary` 分別承接正文與次層資料；附屬資料沿用來源中的 `--ink-muted`，其實際疊層與字級仍需驗證，不能推定為已合格的小字色。
成功／警告／失敗／資訊使用 emerald／amber／rose／indigo，對應 `--telemetry-success/warning/error/info` 與各自 `-bg`。
狀態必須同時有文字或圖示；未知與空值維持中性，不用成功色掩飾沒有資料。
深色畫布與凹面使用前置資料的深色值，卡片與玻璃改為深色透明面；文字、邊線和遙測色使用獨立明度。

**The Solaris Signal Rule.** 暖白、淺灰與黑曜石組織主要視野；橙色集中於品牌、光線及指定交易狀態，功能狀態同時提供文字或圖示。

## Typography

正文與控制重用 `--font-sans`／`--dawn-sans` 的系統與 Inter 字體堆疊；既有 Space Grotesk Variable／Public Sans Variable 保留於盲盒庫存數字等明確指定的展示元件，JetBrains Mono Variable 用於代碼、識別碼與量測數字。
Inter、JetBrains Mono、Public Sans 與 Space Grotesk 由本地 Fontsource 載入；中文使用系統後備。金額、配額、速率和資料表使用等寬數字，不能用字距模擬對齊。前置資料 display 描述指定展示元件的字形，不能推定全站標題皆使用同一字族。
公開入口採較大的展示尺度和留白；首頁專用巨字不延伸到登入、設定、帳務與文件正文。
工作介面採 14–16px 正文與緊湊掃讀；市場及側欄仍保留 10–13px 元資料，不能把它宣稱為全站 16px。
閱讀介面正文 16px、行高約 1.7–1.8，正文欄最大 760px；常見文章標題 32px、區塊標題 24px。
錢包與盲盒區塊標題常用 20px／600；資料標籤採 13–14px，主要餘額與庫存採較大數字，字重與留白承接層級。

## Layout

公開頁與控制台共享七個主要連結，順序為首頁 `/`、模型 `/pricing`、市場 `/market`、文件 `/docs`、社區、排行榜 `/rankings`、控制台 `/dashboard/overview`。
社區指向 `https://community.codegoai.com`；控制台保留登入要求。次要選單收納狀態與聊天；桌面與手機導覽均移除下載與關於。
配置真源為 [top-nav.config.ts](src/components/layout/config/top-nav.config.ts)，視覺真源為 [public-header.css](src/components/layout/components/public-header.css)。
公開導覽為玻璃黏住頂欄，桌面高 76px、容器最大 1680px；1200px 以下高 64px，改用手機選單。
凹面膠囊內的目前頁使用黑曜石選取底與淺字；選取滑移保留彈簧感，減少動態時直接更新。
控制台保留單一視窗工作區與主區內部捲動；[console-shell.css](src/components/layout/components/console-shell.css) 的 Dawn 側欄寬 232px、收起 76px。
首頁保留全寬路由光學場景、品牌字與可操作的路由示範；實際尺寸與斷點以 `market-home.css` 最後生效規則為準。
市場外框最大 1560px；桌面主區加 360px 路由池、間距 28px；1100px 以下池改為底部可折疊工作台。
文件外框最大 1440px，桌面欄位為 216px 導覽、最大 760px 正文、160px 綱要；窄屏改可展開導覽與單欄閱讀。
帳務內容最大 1440px，使用表面分組、獨立數字掃讀軸、可換行操作和局部表格橫滾，不能以整頁橫滾容納資料。
錢包概覽先呈現雙餘額，再以三欄活動資料承接；639px 以下餘額改為單欄，交易紀錄操作保留在概覽表面內。充值與兌換桌面並列，1023px 以下垂直排列。
盲盒工作區主操作加 300px 側欄，1100px 以下側欄為 260px，800px 以下改單欄；庫存與購買同時改為上下排列。獎池的文字機率保持可見，手機可省略裝飾機率條。

## Elevation & Depth

細邊線、下移柔影和高光建立深度；玻璃用於黏住導覽與路由工作台，不降低表單和金額的可讀性。共用表面使用 `--shadow-card`，浮層使用 `--shadow-floating`；帳務普通表面可保持無影，錢包與盲盒主操作使用局部柔影。
光學軌道與短促光線進場延續 Solaris 世界；裝飾不承擔資料與操作，減少動態時保留靜態內容並停止動態。

## Shapes

基礎形狀為面板 20px、控制 12px、緊湊元件 8px；導覽分段、短操作與指定財務操作使用膠囊。
共用 Card／SurfaceCard 維持 20px；Button／Input／Select 使用 12px。錢包餘額、充值、兌換、額度包與盲盒主操作／道具預覽採局部 16px 面板；內部控制和記錄面依既有元件維持較小圓角。

**The Scoped Panel Rule.** 錢包與盲盒的 16px 局部尺度只套用其工作區，共用 Card／SurfaceCard 維持 20px。

## Components

### 錢包與盲盒操作面

錢包以黑曜石雙餘額面、白色充值面與淺灰兌換面分區；盲盒以黑曜石庫存面配白色購買面，獎池、道具和記錄各有明確掃讀區。表面、間距、字重和操作標籤提供層級。
庫存、餘額、數量、單價、應付金額、支付方式、限額與不足狀態使用真實資料；沒有資料時保留明確空值。功能圖示使用 Lucide 的線性 SVG，常用操作為 16px，依元件尺寸縮放；支付、OAuth 與模型供應商品牌標誌可保留既有品牌庫。

### 市場與財務資料完整性

市場倍率、成功率、平均首字、時間窗請求數、平均實扣／1M tokens、快取命中、模型數、並發與評分保持可見。
套餐、遠程壓縮版本、倍率卡、生命週期與近期請求條保持可達；備註和標籤才按需展開，完整模型費率不能被移除。
路由池成員調整維持草稿與明確儲存；連通測試保留模型選取、帳戶額度計費提醒和真實結果，不把視覺回饋當成功證據。
帳單、訂單、退款、訂閱、盲盒與轉帳繼續使用真實 API 與既有金額單位；不在樣式改造中推測餘額、改變計費或結算規則。
帳務共用面板使用 20px 卡片，錢包與盲盒使用上述 16px 局部面板；記錄面與具名狀態保持緊湊。金額、編號、到期日、錯誤與交易後果完整可讀、可複製或可展開。
危險操作保留確認與錯誤；失敗不得靜默呈現為成功；空態與未知狀態不得填入示例指標。
不編造 SLA、零保留、延遲、支援時效或付款成功；獨立文件、法律正文與必要交易說明保留。

### 互動、可達性與語言

共用預設按鈕與輸入高 44px，大按鈕高 48px；手機共用按鈕／選取／頁籤提高最小 44px，checkbox／radio／switch 擴大命中區。
仍須逐頁驗證標籤、鍵盤順序、可見焦點、抽屜還焦、長字串、表格橫滾及真實錯誤；CSS 最小尺寸不等同整頁可達性合格。
普通小字目標至少 4.5:1，大字至少 3:1；基礎色對計算不能替代透明疊層、頁面局部覆寫和實際字級的瀏覽器檢查。
輸入保持可見標籤與錯誤，placeholder 不替代 label；選取文字、游標、捲動條與焦點環均使用語義色。
`--ease-spring` 與 `--ease-optical` 提供短回饋；`prefers-reduced-motion` 停止共用浮層、進度、開關和指定頁面動態。
載入、停用、無資料、錯誤與危險操作保留獨立狀態；資料不能依賴進場動畫才可見。
界面支援繁中、簡中、英文、法文、俄文、日文、越南文；預設繁中，初始載入選中語言與英文後備，其餘按需載入。
重用 CodeGoMark、CompanyIdentity 與公司資料源；功能圖示統一 Lucide，支付、OAuth 及模型供應商品牌可沿用既有品牌資產與按家族懶載的 Lobe。

**The Direct Interface Rule.** 介面以操作標籤和資料建立層次；保留錯誤、安全、交易必需欄位、法律與文件正文，移除說明性副標題、描述段、引導小字及含「不是」的轉折句。

## Do's and Don'ts

### Do:
- **Do** 重用語義變數與既有功能樣式，保留共用 20px 與錢包／盲盒局部 16px 面板的作用域。
- **Do** 以表面、留白與字重組織操作、金額、庫存和交易狀態，功能圖示統一使用 Lucide。
- **Do** 保留操作標籤、真實資料、錯誤、安全、交易必需欄位及法律／文件正文。
- **Do** 逐頁驗證窄屏、長字串、焦點、減少動態與實際錯誤；驗證結論以相應測試證據為準。

### Don't:
- **Don't** 在產品介面新增說明性副標題、描述段、引導小字或含「不是」的轉折句。
- **Don't** 混用功能圖示庫、字元圖示或手繪圖示；支付、OAuth 和模型品牌資產保留既有來源。
- **Don't** 將首頁巨字與路由動態套到表單、文件和帳務，或把局部裝飾色登記為全站語義色。
- **Don't** 編造穩定性、延遲、SLA、金額或交易結果，或把原始碼同步推定為全站可達性及交易驗收通過。
