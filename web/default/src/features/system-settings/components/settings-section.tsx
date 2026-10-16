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
import { createContext, useContext, useId } from 'react'

const SettingsSectionContext = createContext(false)

type SettingsSectionProps = {
  title: string
  description?: string
  titleProps?: React.HTMLAttributes<HTMLHeadingElement>
  children: React.ReactNode
  className?: string
}

export function SettingsSection({
  title,
  titleProps,
  children,
  className,
}: SettingsSectionProps) {
  const isNested = useContext(SettingsSectionContext)
  const headingId = useId()
  const Heading = isNested ? 'h2' : 'h1'
  const baseClassName = 'settings-section'
  const sectionClassName = className
    ? `${baseClassName} ${className}`
    : baseClassName

  return (
    <section className={sectionClassName} aria-labelledby={headingId}>
      <header className='settings-section-header'>
        <Heading
          id={headingId}
          {...titleProps}
          className={
            titleProps?.className
              ? `settings-section-title ${titleProps.className}`
              : 'settings-section-title'
          }
        >
          {title}
        </Heading>
      </header>
      <SettingsSectionContext.Provider value={true}>
        <div className='settings-section-body'>{children}</div>
      </SettingsSectionContext.Provider>
    </section>
  )
}
