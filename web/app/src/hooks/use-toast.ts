import { create } from 'zustand'

type Toast = {
  id: string
  message: string
  variant: 'success' | 'error' | 'info'
}

type ToastStore = {
  toasts: Toast[]
  add: (message: string, variant?: Toast['variant']) => void
  remove: (id: string) => void
}

export const useToast = create<ToastStore>((set) => ({
  toasts: [],
  add: (message, variant = 'info') => {
    const id = Math.random().toString(36).slice(2)
    set((state) => ({ toasts: [...state.toasts, { id, message, variant }] }))
    setTimeout(() => {
      set((state) => ({ toasts: state.toasts.filter((t) => t.id !== id) }))
    }, 3000)
  },
  remove: (id) => set((state) => ({ toasts: state.toasts.filter((t) => t.id !== id) })),
}))
