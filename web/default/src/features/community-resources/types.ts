export type ResourceCategory = 'script' | 'skill' | 'tool' | 'other'
export type ResourceStatus = 'pending' | 'approved' | 'rejected'

export interface CommunityResource {
  id: number
  title: string
  description: string
  category: ResourceCategory
  github_url: string
  repository_url: string
  acknowledgement_url?: string
  download_url: string
  submitted_by: number
  submitter_name: string
  status: ResourceStatus
  review_note?: string
  reward_quota: number
  published_at?: string
  rewarded_at?: string
  created_at: string
  updated_at: string
}

export interface ResourceList {
  items: CommunityResource[]
  total: number
  page: number
  page_size: number
}

export interface ResourceConfig {
  site_host: string
  reward_enabled: boolean
  reward_usd: number
}

export interface ResourceFilters {
  keyword?: string
  category?: ResourceCategory | 'all'
  status?: ResourceStatus | 'all'
  page?: number
}

export interface SubmitResourceInput {
  title: string
  description: string
  category: ResourceCategory
  github_url: string
  acknowledgement_url?: string
}
