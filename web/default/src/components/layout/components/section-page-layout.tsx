/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  Children,
  isValidElement,
  useState,
  type ReactElement,
  type ReactNode,
} from 'react'
import { Main } from './main'
import { PageFooterProvider } from './page-footer'

type SlotProps = { children?: ReactNode }

function SectionPageLayoutTitle(_props: SlotProps) {
  return null
}
SectionPageLayoutTitle.displayName = 'SectionPageLayout.Title'

function SectionPageLayoutDescription(_props: SlotProps) {
  return null
}
SectionPageLayoutDescription.displayName = 'SectionPageLayout.Description'

function SectionPageLayoutActions(_props: SlotProps) {
  return null
}
SectionPageLayoutActions.displayName = 'SectionPageLayout.Actions'

function SectionPageLayoutContent(_props: SlotProps) {
  return null
}
SectionPageLayoutContent.displayName = 'SectionPageLayout.Content'

function SectionPageLayoutBreadcrumb(_props: SlotProps) {
  return null
}
SectionPageLayoutBreadcrumb.displayName = 'SectionPageLayout.Breadcrumb'

export type SectionPageLayoutProps = {
  children: ReactNode
}

export const SECTION_PAGE_LAYOUT_CLASS_NAME =
  'codego-section-page flex h-full min-h-0 flex-1 flex-col overflow-hidden'

export const SECTION_PAGE_CONTENT_CLASS_NAME =
  'codego-page-content min-h-0 flex-1 overflow-auto px-4 pt-5 pb-6 sm:px-7 sm:pt-6 sm:pb-8'

export function SectionPageLayout(props: SectionPageLayoutProps) {
  const [footerContainer, setFooterContainer] = useState<HTMLDivElement | null>(
    null
  )

  let title: ReactNode = null
  let actions: ReactNode = null
  let content: ReactNode = null
  let breadcrumb: ReactNode = null
  const auxiliaryChildren: ReactNode[] = []

  Children.forEach(props.children, (node) => {
    if (!isValidElement(node)) return
    const child = node as ReactElement<SlotProps>
    if (child.type === SectionPageLayoutTitle) title = child.props.children
    else if (child.type === SectionPageLayoutDescription) return
    else if (child.type === SectionPageLayoutActions)
      actions = child.props.children
    else if (child.type === SectionPageLayoutContent)
      content = child.props.children
    else if (child.type === SectionPageLayoutBreadcrumb)
      breadcrumb = child.props.children
    else auxiliaryChildren.push(node)
  })

  return (
    <div className={SECTION_PAGE_LAYOUT_CLASS_NAME}>
      {auxiliaryChildren}
      <PageFooterProvider container={footerContainer}>
        <Main>
          <div className='codego-page-intro shrink-0 px-4 pt-6 pb-6 sm:px-7 sm:pt-8 sm:pb-7'>
            {breadcrumb != null && (
              <div className='codego-page-breadcrumb mb-4'>{breadcrumb}</div>
            )}
            <div className='flex flex-wrap items-end justify-between gap-x-8 gap-y-5'>
              <div className='max-w-3xl min-w-0'>
                <div className='mb-3 flex items-center gap-2.5'>
                  <span className='codego-page-signal' aria-hidden='true' />
                  <span className='codego-page-kicker'>
                    AI CODING GATEWAY · CONSOLE
                  </span>
                </div>
                <h2 className='codego-page-title text-foreground text-3xl leading-[1.04] font-semibold text-balance sm:text-4xl'>
                  {title}
                </h2>
              </div>
              {actions != null && (
                <div className='codego-page-actions flex shrink-0 flex-wrap items-center gap-2'>
                  {actions}
                </div>
              )}
            </div>
          </div>

          <div className={SECTION_PAGE_CONTENT_CLASS_NAME}>{content}</div>

          <div
            ref={setFooterContainer}
            className='bg-background shrink-0 border-t px-3 py-2.5 empty:hidden sm:px-5 sm:py-3'
          />
        </Main>
      </PageFooterProvider>
    </div>
  )
}

SectionPageLayout.Title = SectionPageLayoutTitle
SectionPageLayout.Description = SectionPageLayoutDescription
SectionPageLayout.Actions = SectionPageLayoutActions
SectionPageLayout.Content = SectionPageLayoutContent
SectionPageLayout.Breadcrumb = SectionPageLayoutBreadcrumb
