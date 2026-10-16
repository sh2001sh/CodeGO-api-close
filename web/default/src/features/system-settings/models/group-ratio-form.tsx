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
import { memo, useCallback, useState } from 'react'
import { type UseFormReturn } from 'react-hook-form'
import { Code2, Eye, HelpCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { handleSettingsAction } from '../utils/settings-action'
import { GroupRatioVisualEditor } from './group-ratio-visual-editor'
import { GroupSpecialUsableRulesEditor } from './group-special-usable-editor'
import { SubscriptionGroupPolicyEditor } from './subscription-group-policy-editor'

type GroupFormValues = {
  GroupRatio: string
  TopupGroupRatio: string
  UserUsableGroups: string
  GroupGroupRatio: string
  AutoGroups: string
  DefaultUseAutoGroup: boolean
  GroupSpecialUsableGroup: string
  SubscriptionGroupPolicy: string
}

type GroupRatioFormProps = {
  form: UseFormReturn<GroupFormValues>
  onSave: (values: GroupFormValues) => Promise<void>
  isSaving: boolean
}

export const GroupRatioForm = memo(function GroupRatioForm({
  form,
  onSave,
  isSaving,
}: GroupRatioFormProps) {
  const { t } = useTranslation()
  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const [guideOpen, setGuideOpen] = useState(false)

  const handleFieldChange = useCallback(
    (field: keyof GroupFormValues, value: string) => {
      form.setValue(field, value, {
        shouldValidate: true,
        shouldDirty: true,
      })
    },
    [form]
  )

  const toggleEditMode = useCallback(() => {
    setEditMode((prev) => (prev === 'visual' ? 'json' : 'visual'))
  }, [])

  return (
    <div className='space-y-6'>
      <div className='flex flex-wrap justify-end gap-2'>
        <Button variant='outline' size='sm' onClick={() => setGuideOpen(true)}>
          <HelpCircle className='mr-2 h-4 w-4' />
          {t('Usage guide')}
        </Button>
        <Button variant='outline' size='sm' onClick={toggleEditMode}>
          {editMode === 'visual' ? (
            <>
              <Code2 className='mr-2 h-4 w-4' />
              {t('Switch to JSON')}
            </>
          ) : (
            <>
              <Eye className='mr-2 h-4 w-4' />
              {t('Switch to Visual')}
            </>
          )}
        </Button>
      </div>

      <GroupPricingGuide open={guideOpen} onOpenChange={setGuideOpen} />

      <Form {...form}>
        {editMode === 'visual' ? (
          <div className='space-y-6'>
            <GroupRatioVisualEditor
              groupRatio={form.watch('GroupRatio')}
              topupGroupRatio={form.watch('TopupGroupRatio')}
              userUsableGroups={form.watch('UserUsableGroups')}
              groupGroupRatio={form.watch('GroupGroupRatio')}
              autoGroups={form.watch('AutoGroups')}
              onChange={(field, value) =>
                handleFieldChange(field as keyof GroupFormValues, value)
              }
            />

            <GroupSpecialUsableRulesEditor
              value={form.watch('GroupSpecialUsableGroup')}
              onChange={(value) =>
                handleFieldChange('GroupSpecialUsableGroup', value)
              }
            />

            <SubscriptionGroupPolicyEditor
              groupRatio={form.watch('GroupRatio')}
              value={form.watch('SubscriptionGroupPolicy')}
              onChange={(value) =>
                handleFieldChange('SubscriptionGroupPolicy', value)
              }
            />

            <FormField
              control={form.control}
              name='DefaultUseAutoGroup'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>
                      {t('Default to auto groups')}
                    </FormLabel>
                    <FormDescription className='settings-field-fact'>
                      {t('New API key → auto groups[0]')}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <Button
              onClick={handleSettingsAction(form.handleSubmit(onSave))}
              disabled={isSaving}
            >
              {isSaving ? t('Saving...') : t('Save group ratios')}
            </Button>
          </div>
        ) : (
          <form
            onSubmit={handleSettingsAction(form.handleSubmit(onSave))}
            className='space-y-6'
          >
            <FormField
              control={form.control}
              name='GroupRatio'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Group ratios')}</FormLabel>
                  <FormControl>
                    <Textarea rows={8} {...field} />
                  </FormControl>
                  <FormDescription className='settings-field-fact'>
                    {t('JSON { group: ratio }')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='TopupGroupRatio'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Top-up group ratios')}</FormLabel>
                  <FormControl>
                    <Textarea rows={6} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('JSON { group: ratio }')}
                    {` { "default": 1, "vip": 1.2 }`}.
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='UserUsableGroups'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Selectable groups')}</FormLabel>
                  <FormControl>
                    <Textarea rows={6} {...field} />
                  </FormControl>
                  <FormDescription className='settings-field-fact'>
                    {t('JSON { group: description }')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='GroupGroupRatio'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Inter-group overrides')}</FormLabel>
                  <FormControl>
                    <Textarea rows={8} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('JSON { sourceGroup:')} {`{ targetGroup: ratio }`}{' '}
                    {t('}')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='AutoGroups'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Auto assignment order')}</FormLabel>
                  <FormControl>
                    <Textarea rows={6} {...field} />
                  </FormControl>
                  <FormDescription className='settings-field-fact'>
                    {t('JSON [group] · New API key → auto group')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='GroupSpecialUsableGroup'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Special usable group rules')}</FormLabel>
                  <FormControl>
                    <Textarea rows={8} {...field} />
                  </FormControl>
                  <FormDescription className='settings-field-fact'>
                    {t('JSON { group: rules } · +: / -:')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='SubscriptionGroupPolicy'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Subscription billing')}</FormLabel>
                  <FormControl>
                    <Textarea rows={8} {...field} />
                  </FormControl>
                  <FormDescription className='settings-field-fact'>
                    {t('JSON { group: { enabled, ratio } }')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='DefaultUseAutoGroup'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>
                      {t('Default to auto groups')}
                    </FormLabel>
                    <FormDescription className='settings-field-fact'>
                      {t('New API key → auto groups[0]')}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <div className='settings-save-bar'>
              <Button type='submit' disabled={isSaving}>
                {isSaving ? t('Saving...') : t('Save group ratios')}
              </Button>
            </div>
          </form>
        )}
      </Form>
    </div>
  )
})

type GroupPricingGuideProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function GuideCodeBlock({ children }: { children: string }) {
  return (
    <pre className='bg-muted/60 overflow-x-auto rounded-lg border px-3 py-2 text-xs leading-6 whitespace-pre-wrap'>
      {children}
    </pre>
  )
}

function GroupPricingGuide({ open, onOpenChange }: GroupPricingGuideProps) {
  const { t } = useTranslation()

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side='right'
        className='settings-dialog w-full gap-0 p-0 sm:max-w-2xl'
      >
        <SheetHeader className='border-b p-4'>
          <SheetTitle>{t('Group pricing usage guide')}</SheetTitle>
          <SheetDescription>
            {t(
              'Understand how user groups, token groups, ratios, and special rules work together.'
            )}
          </SheetDescription>
        </SheetHeader>

        <div className='space-y-5 overflow-y-auto p-4'>
          <section className='space-y-2'>
            <h3 className='text-sm font-semibold'>{t('Core concepts')}</h3>
            <div className='text-muted-foreground space-y-2 text-sm leading-6'>
              <p>
                <span className='text-foreground font-medium'>
                  {t('User group')}
                </span>
                {': '}
                {t(
                  'Assigned by administrators and used to represent a user level, such as default or vip.'
                )}
              </p>
              <p>
                <span className='text-foreground font-medium'>
                  {t('Token group')}
                </span>
                {': '}
                {t(
                  'Selected when creating a token and used as the default billing group for API calls.'
                )}
              </p>
              <p>
                <span className='text-foreground font-medium'>
                  {t('Ratio')}
                </span>
                {': '}
                {t(
                  'A billing multiplier. Lower ratios mean lower API call costs.'
                )}
              </p>
              <p>
                <span className='text-foreground font-medium'>
                  {t('User selectable')}
                </span>
                {': '}
                {t(
                  'When enabled, users can pick this group when creating tokens.'
                )}
              </p>
            </div>
          </section>

          <Accordion className='rounded-lg border px-3'>
            <AccordionItem value='groups'>
              <AccordionTrigger>{t('Pricing group example')}</AccordionTrigger>
              <AccordionContent className='space-y-3'>
                <GuideCodeBlock>
                  {`${t('Group name')}   ${t('Ratio')}   ${t('User selectable')}   ${t('Description')}
standard     1.0     ${t('Yes')}               ${t('Standard price')}
premium      0.5     ${t('Yes')}               ${t('Premium plan, half price')}
vip          0.5     ${t('No')}                ${t('Assigned by administrator only')}`}
                </GuideCodeBlock>
                <p className='text-muted-foreground text-base leading-6'>
                  {t('User selectable: visible · Admin: all groups')}
                </p>
              </AccordionContent>
            </AccordionItem>

            <AccordionItem value='auto'>
              <AccordionTrigger>{t('Auto group behavior')}</AccordionTrigger>
              <AccordionContent className='space-y-3'>
                <p className='text-muted-foreground text-base leading-6'>
                  {t('auto: first available group, top → bottom')}
                </p>
                <GuideCodeBlock>{`["default", "vip"]`}</GuideCodeBlock>
                <p className='text-muted-foreground text-base leading-6'>
                  {t('Default auto group: new API key → auto')}
                </p>
              </AccordionContent>
            </AccordionItem>

            <AccordionItem value='special-ratio'>
              <AccordionTrigger>{t('Special ratio rules')}</AccordionTrigger>
              <AccordionContent className='space-y-3'>
                <p className='text-muted-foreground text-base leading-6'>
                  {t('User group × Token group → Special ratio')}
                </p>
                <GuideCodeBlock>{`{
  "vip": {
    "standard": 0.8,
    "premium": 0.3
  }
}`}</GuideCodeBlock>
                <p className='text-muted-foreground text-base leading-6'>
                  {t('Other calls → Base ratio')}
                </p>
              </AccordionContent>
            </AccordionItem>

            <AccordionItem value='usable'>
              <AccordionTrigger>
                {t('Special usable group rules')}
              </AccordionTrigger>
              <AccordionContent className='space-y-3'>
                <p className='text-muted-foreground text-base leading-6'>
                  {t('User group → Selectable token groups')}
                </p>
                <GuideCodeBlock>{`{
  "vip": {
    "+:premium": "${t('Premium plan, half price')}",
    "-:default": "remove",
    "special": "${t('Special group')}"
  }
}`}</GuideCodeBlock>
                <p className='text-muted-foreground text-base leading-6'>
                  {t('+: add · -: remove · No prefix: append')}
                </p>
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        </div>
      </SheetContent>
    </Sheet>
  )
}
