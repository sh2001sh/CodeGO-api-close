import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Gift } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { IconGithub } from '@/assets/brand-icons'
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
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import type { SubmitResourceInput } from './types'

const schema = z.object({
  title: z.string().trim().min(2).max(80),
  description: z.string().trim().min(10).max(500),
  category: z.enum(['script', 'skill', 'tool', 'other']),
  github_url: z.string().url().startsWith('https://github.com/'),
  acknowledgement_url: z
    .string()
    .trim()
    .refine(
      (value) => !value || value.startsWith('https://github.com/'),
      'Use a GitHub URL'
    ),
})

export function ResourceSubmitSheet(props: {
  open: boolean
  pending: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (input: SubmitResourceInput) => void
}) {
  const { t } = useTranslation()
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: {
      title: '',
      description: '',
      category: 'skill',
      github_url: '',
      acknowledgement_url: '',
    },
  })

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='w-full sm:max-w-lg'>
        <SheetHeader className='border-b p-5'>
          <SheetTitle>{t('Submit a community resource')}</SheetTitle>
          <SheetDescription>
            {t(
              'Share a GitHub-hosted script, skill, or tool with the community.'
            )}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='community-resource-form'
            onSubmit={form.handleSubmit((values) =>
              props.onSubmit({
                ...values,
                acknowledgement_url: values.acknowledgement_url || undefined,
              })
            )}
            className='flex-1 space-y-5 overflow-y-auto px-5 py-1'
          >
            <FormField
              control={form.control}
              name='title'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Resource name')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('Example: Codex setup skill')}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='category'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Category')}</FormLabel>
                  <FormControl>
                    <NativeSelect
                      className='w-full'
                      value={field.value}
                      onChange={field.onChange}
                    >
                      <NativeSelectOption value='script'>
                        {t('Script')}
                      </NativeSelectOption>
                      <NativeSelectOption value='skill'>
                        {t('Skill')}
                      </NativeSelectOption>
                      <NativeSelectOption value='tool'>
                        {t('Tool')}
                      </NativeSelectOption>
                      <NativeSelectOption value='other'>
                        {t('Other')}
                      </NativeSelectOption>
                    </NativeSelect>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='description'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Description')}</FormLabel>
                  <FormControl>
                    <Textarea
                      rows={5}
                      placeholder={t(
                        'What does this resource help users accomplish?'
                      )}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Keep setup requirements and supported tools clear.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='github_url'
              render={({ field }) => (
                <FormItem>
                  <FormLabel className='flex items-center gap-2'>
                    <IconGithub
                      className='size-4'
                      aria-hidden='true'
                      focusable='false'
                    />
                    {t('GitHub URL')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      placeholder='https://github.com/owner/repository'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Repository and subdirectory links are accepted. Downloads use the repository’s default branch.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='acknowledgement_url'
              render={({ field }) => (
                <FormItem className='border-primary/20 bg-primary/5 rounded-lg border p-4'>
                  <FormLabel className='flex items-center gap-2'>
                    <Gift className='text-primary size-4' />
                    {t('shu26.cfd acknowledgement link')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      placeholder='https://github.com/owner/repository/blob/main/README.md'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Optional. Link to the README or project file that thanks shu26.cfd. An administrator can verify it and grant bonus quota.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </form>
        </Form>
        <SheetFooter className='border-t p-5 sm:flex-row sm:justify-end'>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='community-resource-form'
            disabled={props.pending}
          >
            {props.pending
              ? t('Submitting resource...')
              : t('Submit resource for review')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
