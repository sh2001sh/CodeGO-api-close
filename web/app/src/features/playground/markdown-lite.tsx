import { memo, useMemo } from 'react'
import { CopyButton } from '../../components/ui'
import { markdownBlocks } from './markdown-parser'
import './markdown.css'

/** React text nodes escape HTML; unchanged historical messages never parse again. */
export const MarkdownLite = memo(function MarkdownLite({ content }: { content: string }) {
  const blocks = useMemo(() => markdownBlocks(content), [content])
  return (
    <div className="chat-markdown">
      {blocks.map((block, index) => (
        <div key={index}>
          {block.kind === 'code' ? (
            <div className="code-block" dir="ltr">
              {block.language && <span className="chat-code-language">{block.language}</span>}
              <CopyButton value={block.content} label="复制代码" />
              <pre>
                <code>{block.content}</code>
              </pre>
            </div>
          ) : (
            block.node
          )}
        </div>
      ))}
    </div>
  )
})
