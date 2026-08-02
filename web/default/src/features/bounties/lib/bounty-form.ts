import { z } from 'zod'

export const bountyFormSchema = z.object({
  title: z
    .string()
    .trim()
    .min(4, 'Title must be at least 4 characters')
    .max(80, 'Title must be at most 80 characters'),
  description: z
    .string()
    .trim()
    .min(12, 'Add the task background and goal')
    .max(20000, 'Description must be at most 20000 characters'),
  repo_url: z
    .string()
    .url('Enter a GitHub repository, Issue, or project URL')
    .refine((value) => /^https:\/\/(www\.)?github\.com\//.test(value), {
      message: 'Use an https://github.com/ URL',
    }),
  task_type: z.enum(['general', 'ui', 'frontend', 'backend']),
  tags: z.string().optional(),
  reward_wallet_type: z.enum(['wallet', 'claude_wallet']),
  reward_amount: z.number().min(0.01, 'Reward must be at least $0.01'),
  deadline_at: z
    .string()
    .min(1, 'Select a delivery deadline')
    .refine((value) => new Date(value).getTime() > Date.now(), {
      message: 'Deadline must be later than now',
    }),
})

export type BountyFormValues = z.infer<typeof bountyFormSchema>

export function tagsFromInput(value: string | undefined) {
  return (value ?? '')
    .split(',')
    .map((tag) => tag.trim())
    .filter(Boolean)
    .slice(0, 12)
}
