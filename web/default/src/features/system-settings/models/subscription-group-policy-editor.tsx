import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { safeJsonParse } from '../utils/json-parser'

type SubscriptionGroupPolicy = {
  enabled: boolean
  multiplier: number
}

type SubscriptionGroupPolicyEditorProps = {
  groupRatio: string
  value: string
  onChange: (value: string) => void
}

function normalizePolicy(value?: Partial<SubscriptionGroupPolicy>) {
  const multiplier = Number(value?.multiplier)
  return {
    enabled: value?.enabled === true,
    multiplier: Number.isFinite(multiplier) && multiplier > 0 ? multiplier : 1,
  }
}

export function SubscriptionGroupPolicyEditor({
  groupRatio,
  value,
  onChange,
}: SubscriptionGroupPolicyEditorProps) {
  const { t } = useTranslation()
  const groups = useMemo(
    () =>
      Object.keys(
        safeJsonParse<Record<string, number>>(groupRatio, {
          fallback: {},
          context: 'group ratios',
        })
      ),
    [groupRatio]
  )
  const policies = useMemo(
    () =>
      safeJsonParse<Record<string, SubscriptionGroupPolicy>>(value, {
        fallback: {},
        context: 'subscription group policies',
      }),
    [value]
  )

  const updatePolicy = (
    group: string,
    patch: Partial<SubscriptionGroupPolicy>
  ) => {
    const next = Object.fromEntries(
      groups.map((name) => [name, normalizePolicy(policies[name])])
    )
    next[group] = { ...normalizePolicy(next[group]), ...patch }
    onChange(JSON.stringify(next, null, 2))
  }

  return (
    <Card className='before:border-border/90 relative shadow-sm ring-0 before:pointer-events-none before:absolute before:inset-0 before:rounded-xl before:border'>
      <CardHeader className='bg-muted/20 border-b'>
        <CardTitle>{t('Monthly pass billing')}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className='overflow-hidden rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='min-w-40'>{t('Group name')}</TableHead>
                <TableHead className='w-32 text-center'>
                  {t('Allow monthly pass')}
                </TableHead>
                <TableHead className='w-36'>
                  {t('Monthly pass multiplier')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {groups.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={3}
                    className='text-muted-foreground h-20 text-center text-sm'
                  >
                    {t('No groups configured')}
                  </TableCell>
                </TableRow>
              ) : (
                groups.map((group) => {
                  const policy = normalizePolicy(policies[group])
                  return (
                    <TableRow key={group}>
                      <TableCell className='font-medium'>{group}</TableCell>
                      <TableCell>
                        <div className='flex justify-center'>
                          <Checkbox
                            checked={policy.enabled}
                            onCheckedChange={(checked) =>
                              updatePolicy(group, {
                                enabled: checked === true,
                              })
                            }
                            aria-label={t('Allow monthly pass for {{group}}', {
                              group,
                            })}
                          />
                        </div>
                      </TableCell>
                      <TableCell>
                        <Input
                          type='number'
                          min={0.01}
                          step={0.01}
                          value={String(policy.multiplier)}
                          disabled={!policy.enabled}
                          onChange={(event) =>
                            updatePolicy(group, {
                              multiplier: normalizePolicy({
                                multiplier: event.target.valueAsNumber,
                              }).multiplier,
                            })
                          }
                          aria-label={t(
                            'Monthly pass multiplier for {{group}}',
                            { group }
                          )}
                        />
                      </TableCell>
                    </TableRow>
                  )
                })
              )}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  )
}
