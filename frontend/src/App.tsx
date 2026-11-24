import { useEffect, useRef, useState, type ChangeEvent, type FormEvent } from 'react'

type Health = {
  status: string
  dependencies: { postgres: string; redis: string }
}

type User = {
  id: number
  email: string
  name: string
  role: 'user' | 'admin'
  created_at?: string
}

type AuthResponse = {
  user: User
  access_token: string
  refresh_token: string
  token_type: 'Bearer'
  expires_in: number
}

type Session = AuthResponse
type AuthMode = 'login' | 'register'

type Category = {
  id: number
  name: string
  slug: string
  description: string
}

type Product = {
  id: number
  category_id: number
  category: string
  name: string
  slug: string
  description: string
  price: number
  stock: number
  reserved_stock: number
  available_stock: number
  image_url: string
  is_active: boolean
}

type ProductPage = { data: Product[]; page: number; limit: number; total: number }

type CartLine = {
  product_id: number
  name: string
  slug: string
  image_url: string
  price: number
  stock: number
  quantity: number
  line_total: number
}

type Cart = { items: CartLine[]; total: number; item_count: number }
type OrderStatus = 'confirmed' | 'processing' | 'shipped' | 'delivered' | 'cancelled'
type OrderLine = { id: number; product_id: number; product_name: string; product_slug: string; image_url: string; unit_price: number; quantity: number; line_total: number }
type Order = {
  id: number
  code: string
  user_id: number
  email: string
  recipient_name: string
  phone: string
  address: string
  note: string
  payment_method: string
  payment_status: string
  status: OrderStatus
  subtotal: number
  shipping_fee: number
  total: number
  items?: OrderLine[]
  status_history?: { from_status?: string; to_status: OrderStatus; changed_by?: number; created_at: string }[]
  created_at: string
}
type OrderPage = { data: Order[]; page: number; limit: number; total: number }
type ChatConversation = { id: number; owner_id: number; owner_name: string; order_id?: number; product_id?: number; product_name?: string; last_message?: string; last_message_at: string; created_at: string }
type ChatMessage = { id: number; conversation_id: number; sender_id: number; sender_name: string; body: string; created_at: string }

const SESSION_KEY = 'localmarket.auth'
const emptyCart: Cart = { items: [], total: 0, item_count: 0 }

function formatMoney(value: number) { return `${value.toLocaleString('vi-VN')}đ` }

function orderStatusLabel(status: OrderStatus) {
  return ({ confirmed: 'Đã xác nhận', processing: 'Đang chuẩn bị', shipped: 'Đang giao', delivered: 'Đã giao', cancelled: 'Đã hủy' })[status]
}

function saveSession(session: Session | null) {
  if (session) sessionStorage.setItem(SESSION_KEY, JSON.stringify(session))
  else sessionStorage.removeItem(SESSION_KEY)
}

async function readError(response: Response) {
  try {
    const body = (await response.json()) as { error?: string }
    return body.error ?? 'Có lỗi xảy ra. Vui lòng thử lại.'
  } catch {
    return 'Có lỗi xảy ra. Vui lòng thử lại.'
  }
}

export default function App() {
  const [health, setHealth] = useState<Health | null>(null)
  const [healthError, setHealthError] = useState(false)
  const [session, setSession] = useState<Session | null>(null)
  const [authMode, setAuthMode] = useState<AuthMode>('login')
  const [authOpen, setAuthOpen] = useState(false)
  const [authBusy, setAuthBusy] = useState(false)
  const [authError, setAuthError] = useState('')
  const [notice, setNotice] = useState('')
  const sessionRef = useRef<Session | null>(null)
  const checkoutKeyRef = useRef('')
  const [categoriesData, setCategoriesData] = useState<Category[]>([])
  const [productsData, setProductsData] = useState<ProductPage>({ data: [], page: 1, limit: 8, total: 0 })
  const [searchInput, setSearchInput] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [categoryFilter, setCategoryFilter] = useState('')
  const [sortBy, setSortBy] = useState('created_at')
  const [productPage, setProductPage] = useState(1)
  const [stockRefresh, setStockRefresh] = useState(0)
  const [productsLoading, setProductsLoading] = useState(true)
  const [catalogError, setCatalogError] = useState('')
  const [adminOpen, setAdminOpen] = useState(false)
  const [adminProducts, setAdminProducts] = useState<ProductPage>({ data: [], page: 1, limit: 8, total: 0 })
  const [adminPage, setAdminPage] = useState(1)
  const [adminReload, setAdminReload] = useState(0)
  const [adminError, setAdminError] = useState('')
  const [productEditor, setProductEditor] = useState<Product | 'new' | null>(null)
  const [categoryEditor, setCategoryEditor] = useState<Category | 'new' | null>(null)
  const [editorBusy, setEditorBusy] = useState(false)
  const [uploadedImage, setUploadedImage] = useState('')
  const [imagePreview, setImagePreview] = useState('')
  const [imageBusy, setImageBusy] = useState(false)
  const [cart, setCart] = useState<Cart>(emptyCart)
  const [cartOpen, setCartOpen] = useState(false)
  const [checkoutOpen, setCheckoutOpen] = useState(false)
  const [checkoutBusy, setCheckoutBusy] = useState(false)
  const [checkoutError, setCheckoutError] = useState('')
  const [ordersOpen, setOrdersOpen] = useState(false)
  const [orderPage, setOrderPage] = useState<OrderPage>({ data: [], page: 1, limit: 8, total: 0 })
  const [orderPageNumber, setOrderPageNumber] = useState(1)
  const [orderReload, setOrderReload] = useState(0)
  const [orderError, setOrderError] = useState('')
  const [selectedOrder, setSelectedOrder] = useState<Order | null>(null)
  const [selectedOrderAdmin, setSelectedOrderAdmin] = useState(false)
  const [adminOrderPage, setAdminOrderPage] = useState<OrderPage>({ data: [], page: 1, limit: 8, total: 0 })
  const [adminOrderPageNumber, setAdminOrderPageNumber] = useState(1)
  const [adminOrderStatus, setAdminOrderStatus] = useState('')
  const [adminOrderSearch, setAdminOrderSearch] = useState('')
  const [adminOrderSearchInput, setAdminOrderSearchInput] = useState('')
  const [adminOrderReload, setAdminOrderReload] = useState(0)
  const [chatOpen, setChatOpen] = useState(false)
  const [chatConversations, setChatConversations] = useState<ChatConversation[]>([])
  const [chatConversationID, setChatConversationID] = useState<number | null>(null)
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([])
  const [chatDraft, setChatDraft] = useState('')
  const [chatError, setChatError] = useState('')
  const [chatBusy, setChatBusy] = useState(false)
  const [chatConnected, setChatConnected] = useState(false)
  const chatListRef = useRef<HTMLDivElement | null>(null)
  const lastChatMessageID = useRef(0)

  useEffect(() => { sessionRef.current = session }, [session])

  async function authorizedFetch(path: string, init: RequestInit = {}) {
    const current = sessionRef.current
    if (!current) throw new Error('Vui lòng đăng nhập với tài khoản admin.')
    const send = (accessToken: string) => {
      const headers = new Headers(init.headers)
      headers.set('Authorization', `Bearer ${accessToken}`)
      return fetch(path, { ...init, headers })
    }
    let response = await send(current.access_token)
    if (response.status !== 401) return response

    const refreshed = await fetch('/api/v1/auth/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: current.refresh_token }),
    })
    if (!refreshed.ok) {
      saveSession(null)
      sessionRef.current = null
      setSession(null)
      throw new Error('Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.')
    }
    const next = (await refreshed.json()) as Session
    sessionRef.current = next
    saveSession(next)
    setSession(next)
    response = await send(next.access_token)
    return response
  }

  useEffect(() => {
    fetch('/api/v1/health')
      .then(async (response) => {
        if (!response.ok) throw new Error('API unavailable')
        return response.json() as Promise<Health>
      })
      .then(setHealth)
      .catch(() => setHealthError(true))

    const stored = sessionStorage.getItem(SESSION_KEY)
    if (!stored) return

    let previous: Session
    try {
      previous = JSON.parse(stored) as Session
    } catch {
      saveSession(null)
      return
    }

    const loadUser = (accessToken: string) => fetch('/api/v1/auth/me', {
      headers: { Authorization: `Bearer ${accessToken}` },
    })

    loadUser(previous.access_token)
      .then(async (response) => {
        if (response.ok) {
          const body = (await response.json()) as { user: User }
          setSession({ ...previous, user: body.user })
          return
        }

        const refreshed = await fetch('/api/v1/auth/refresh', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ refresh_token: previous.refresh_token }),
        })
        if (!refreshed.ok) throw new Error('Session expired')
        const next = (await refreshed.json()) as Session
        const currentUser = await loadUser(next.access_token)
        if (!currentUser.ok) throw new Error('Session expired')
        const body = (await currentUser.json()) as { user: User }
        const active = { ...next, user: body.user }
        saveSession(active)
        setSession(active)
      })
      .catch(() => saveSession(null))
  }, [])

  useEffect(() => {
    fetch('/api/v1/categories')
      .then(async (response) => {
        if (!response.ok) throw new Error(await readError(response))
        return response.json() as Promise<{ data: Category[] }>
      })
      .then((body) => setCategoriesData(body.data))
      .catch((error) => setCatalogError(error instanceof Error ? error.message : 'Không tải được danh mục.'))
  }, [])

  useEffect(() => {
    const params = new URLSearchParams({ page: String(productPage), limit: '8', q: searchQuery, sort_by: sortBy, sort_order: sortBy === 'created_at' ? 'desc' : 'asc' })
    if (categoryFilter) params.set('category_id', categoryFilter)
    setProductsLoading(true)
    fetch(`/api/v1/products?${params.toString()}`)
      .then(async (response) => {
        if (!response.ok) throw new Error(await readError(response))
        return response.json() as Promise<ProductPage>
      })
      .then(setProductsData)
      .catch((error) => setCatalogError(error instanceof Error ? error.message : 'Không tải được sản phẩm.'))
      .finally(() => setProductsLoading(false))
  }, [productPage, searchQuery, categoryFilter, sortBy, stockRefresh])

  useEffect(() => {
    if (!adminOpen || session?.user.role !== 'admin') return
    const params = new URLSearchParams({ page: String(adminPage), limit: '8', q: searchQuery })
    authorizedFetch(`/api/v1/admin/products?${params.toString()}`)
      .then(async (response) => {
        if (!response.ok) throw new Error(await readError(response))
        return response.json() as Promise<ProductPage>
      })
      .then(setAdminProducts)
      .catch((error) => setAdminError(error instanceof Error ? error.message : 'Không tải được dữ liệu quản trị.'))
  }, [adminOpen, adminPage, adminReload, searchQuery, session?.user.role])

  useEffect(() => {
    if (!session) {
      setCart(emptyCart)
      return
    }
    authorizedFetch('/api/v1/cart')
      .then(async (response) => {
        if (!response.ok) throw new Error(await readError(response))
        return response.json() as Promise<Cart>
      })
      .then((nextCart) => { setCart(nextCart); setStockRefresh((value) => value + 1) })
      .catch(() => setCart(emptyCart))
  }, [session?.user.id])

  useEffect(() => {
    if (!ordersOpen || !session) return
    const params = new URLSearchParams({ page: String(orderPageNumber), limit: '8' })
    authorizedFetch(`/api/v1/orders?${params.toString()}`)
      .then(async (response) => {
        if (!response.ok) throw new Error(await readError(response))
        return response.json() as Promise<OrderPage>
      })
      .then(setOrderPage)
      .catch((error) => setOrderError(error instanceof Error ? error.message : 'Không tải được đơn hàng.'))
  }, [ordersOpen, orderPageNumber, orderReload, session?.user.id])

  useEffect(() => {
    if (!adminOpen || session?.user.role !== 'admin') return
    const params = new URLSearchParams({ page: String(adminOrderPageNumber), limit: '8', q: adminOrderSearch })
    if (adminOrderStatus) params.set('status', adminOrderStatus)
    authorizedFetch(`/api/v1/admin/orders?${params.toString()}`)
      .then(async (response) => {
        if (!response.ok) throw new Error(await readError(response))
        return response.json() as Promise<OrderPage>
      })
      .then(setAdminOrderPage)
      .catch((error) => setOrderError(error instanceof Error ? error.message : 'Không tải được đơn hàng.'))
  }, [adminOpen, adminOrderPageNumber, adminOrderReload, adminOrderSearch, adminOrderStatus, session?.user.role])

  useEffect(() => {
    if (!chatOpen || !session) return
    let active = true
    authorizedFetch('/api/v1/chat/conversations')
      .then(async (response) => { if (!response.ok) throw new Error(await readError(response)); return response.json() as Promise<{ data: ChatConversation[] }> })
      .then(({ data }) => { if (!active) return; setChatConversations(data); if (!chatConversationID && data.length) setChatConversationID(data[0].id) })
      .catch((error) => { if (active) setChatError(error instanceof Error ? error.message : 'Không tải được hội thoại.') })
    return () => { active = false }
  }, [chatOpen, session?.user.id, chatConversationID])

  useEffect(() => {
    if (!chatOpen || !session || !chatConversationID) { setChatMessages([]); setChatConnected(false); return }
    let active = true
    let socket: WebSocket | null = null
    let reconnectTimer = 0
    authorizedFetch(`/api/v1/chat/conversations/${chatConversationID}/messages`)
      .then(async (response) => { if (!response.ok) throw new Error(await readError(response)); return response.json() as Promise<{ data: ChatMessage[] }> })
      .then(({ data }) => { if (active) setChatMessages(data) })
      .catch((error) => { if (active) setChatError(error instanceof Error ? error.message : 'Không tải được lịch sử tin nhắn.') })
    const connectRealtime = async () => {
      try {
        const response = await authorizedFetch('/api/v1/chat/ticket', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ conversation_id: chatConversationID }) })
        if (!response.ok) throw new Error(await readError(response))
        const { ticket } = await response.json() as { ticket: string }
        if (!active) return
        const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        socket = new WebSocket(`${scheme}//${window.location.host}/api/v1/chat/ws?ticket=${encodeURIComponent(ticket)}`)
        socket.onopen = () => { if (active) setChatConnected(true) }
        socket.onclose = () => { if (active) { setChatConnected(false); reconnectTimer = window.setTimeout(connectRealtime, 2000) } }
        socket.onerror = () => { if (active) setChatConnected(false) }
        socket.onmessage = (event) => {
          try {
            const message = JSON.parse(String(event.data)) as ChatMessage
            if (active) setChatMessages((current) => current.some((item) => item.id === message.id) ? current : [...current, message])
          } catch { /* Ignore malformed events and keep the chat open. */ }
        }
      } catch (error) {
        if (active) { setChatConnected(false); setChatError(error instanceof Error ? error.message : 'Không thể kết nối chat realtime.'); reconnectTimer = window.setTimeout(connectRealtime, 3000) }
      }
    }
    void connectRealtime()
    return () => { active = false; window.clearTimeout(reconnectTimer); socket?.close() }
  }, [chatOpen, chatConversationID, session?.user.id])

  useEffect(() => {
    const latestID = chatMessages.length ? chatMessages[chatMessages.length - 1].id : 0
    if (latestID !== lastChatMessageID.current && chatListRef.current) chatListRef.current.scrollTop = chatListRef.current.scrollHeight
    lastChatMessageID.current = latestID
  }, [chatMessages])

  async function submitAuth(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setAuthBusy(true)
    setAuthError('')
    const values = new FormData(event.currentTarget)
    const isRegister = authMode === 'register'
    const body = isRegister
      ? { name: String(values.get('name') ?? '').trim(), email: String(values.get('email') ?? '').trim(), password: String(values.get('password') ?? '') }
      : { email: String(values.get('email') ?? '').trim(), password: String(values.get('password') ?? '') }

    try {
      const response = await fetch(`/api/v1/auth/${isRegister ? 'register' : 'login'}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!response.ok) throw new Error(await readError(response))
      const next = (await response.json()) as Session
      sessionRef.current = next
      saveSession(next)
      setSession(next)
      setAuthOpen(false)
      setNotice(isRegister ? 'Tài khoản của bạn đã được tạo.' : `Xin chào ${next.user.name}!`)
      window.setTimeout(() => setNotice(''), 4000)
    } catch (error) {
      setAuthError(error instanceof Error ? error.message : 'Không thể kết nối máy chủ.')
    } finally {
      setAuthBusy(false)
    }
  }

  async function logout() {
    if (session) {
      await fetch('/api/v1/auth/logout', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: session.refresh_token }),
      }).catch(() => undefined)
    }
    saveSession(null)
    sessionRef.current = null
    setSession(null)
    setChatOpen(false)
    setChatConversationID(null)
    setChatConversations([])
    setChatMessages([])
    setNotice('Bạn đã đăng xuất.')
    window.setTimeout(() => setNotice(''), 4000)
  }

  function openAuth(mode: AuthMode) {
    setAuthMode(mode)
    setAuthError('')
    setAuthOpen(true)
  }

  async function reloadCategories() {
    const response = await fetch('/api/v1/categories')
    if (!response.ok) throw new Error(await readError(response))
    const body = (await response.json()) as { data: Category[] }
    setCategoriesData(body.data)
  }

  async function submitCategory(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!categoryEditor) return
    setEditorBusy(true)
    setAdminError('')
    const values = new FormData(event.currentTarget)
    const body = {
      name: String(values.get('name') ?? '').trim(),
      slug: String(values.get('slug') ?? '').trim(),
      description: String(values.get('description') ?? '').trim(),
    }
    try {
      const editing = categoryEditor !== 'new'
      const response = await authorizedFetch(`/api/v1/admin/categories${editing ? `/${categoryEditor.id}` : ''}`, {
        method: editing ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!response.ok) throw new Error(await readError(response))
      await reloadCategories()
      setCategoryEditor(null)
      setNotice(editing ? 'Đã cập nhật danh mục.' : 'Đã tạo danh mục.')
      window.setTimeout(() => setNotice(''), 4000)
    } catch (error) {
      setAdminError(error instanceof Error ? error.message : 'Không thể lưu danh mục.')
    } finally {
      setEditorBusy(false)
    }
  }

  async function deleteCategory(category: Category) {
    if (!window.confirm(`Xóa danh mục “${category.name}”? Danh mục đang có sản phẩm sẽ không thể xóa.`)) return
    try {
      const response = await authorizedFetch(`/api/v1/admin/categories/${category.id}`, { method: 'DELETE' })
      if (!response.ok) throw new Error(await readError(response))
      await reloadCategories()
      setNotice('Đã xóa danh mục.')
      window.setTimeout(() => setNotice(''), 4000)
    } catch (error) {
      setAdminError(error instanceof Error ? error.message : 'Không thể xóa danh mục.')
    }
  }

  async function uploadProductImage(event: ChangeEvent<HTMLInputElement>) {
    const file = event.currentTarget.files?.[0]
    if (!file) return
    if (!['image/jpeg', 'image/png', 'image/gif'].includes(file.type)) {
      setAdminError('Chỉ chấp nhận ảnh JPEG, PNG hoặc GIF.')
      event.currentTarget.value = ''
      return
    }
    if (file.size > 5 * 1024 * 1024) {
      setAdminError('Ảnh không được vượt quá 5 MB.')
      event.currentTarget.value = ''
      return
    }
    setAdminError('')
    setImagePreview(URL.createObjectURL(file))
    setImageBusy(true)
    const form = new FormData()
    form.append('image', file)
    try {
      const response = await authorizedFetch('/api/v1/admin/uploads', { method: 'POST', body: form })
      if (!response.ok) throw new Error(await readError(response))
      const result = (await response.json()) as { url: string }
      setUploadedImage(result.url)
    } catch (error) {
      setImagePreview('')
      setAdminError(error instanceof Error ? error.message : 'Không thể tải ảnh lên.')
    } finally {
      setImageBusy(false)
    }
  }

  async function submitProduct(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!productEditor) return
    setEditorBusy(true)
    setAdminError('')
    const values = new FormData(event.currentTarget)
    const editing = productEditor !== 'new'
    const body = {
      category_id: Number(values.get('category_id')),
      name: String(values.get('name') ?? '').trim(),
      slug: String(values.get('slug') ?? '').trim(),
      description: String(values.get('description') ?? '').trim(),
      price: Number(values.get('price')),
      stock: Number(values.get('stock')),
      image_url: uploadedImage,
      is_active: values.get('is_active') === 'on',
    }
    try {
      const response = await authorizedFetch(`/api/v1/admin/products${editing ? `/${productEditor.id}` : ''}`, {
        method: editing ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!response.ok) throw new Error(await readError(response))
      setProductEditor(null)
      setImagePreview('')
      setUploadedImage('')
      setAdminReload((value) => value + 1)
      setNotice(editing ? 'Đã cập nhật sản phẩm.' : 'Đã tạo sản phẩm.')
      window.setTimeout(() => setNotice(''), 4000)
    } catch (error) {
      setAdminError(error instanceof Error ? error.message : 'Không thể lưu sản phẩm.')
    } finally {
      setEditorBusy(false)
    }
  }

  async function deleteProduct(product: Product) {
    if (!window.confirm(`Xóa sản phẩm “${product.name}”?`)) return
    try {
      const response = await authorizedFetch(`/api/v1/admin/products/${product.id}`, { method: 'DELETE' })
      if (!response.ok) throw new Error(await readError(response))
      setAdminReload((value) => value + 1)
      setNotice('Đã xóa sản phẩm.')
      window.setTimeout(() => setNotice(''), 4000)
    } catch (error) {
      setAdminError(error instanceof Error ? error.message : 'Không thể xóa sản phẩm.')
    }
  }

  async function addCartItem(product: Product) {
    if (!sessionRef.current) {
      openAuth('login')
      return
    }
    try {
      const response = await authorizedFetch('/api/v1/cart/items', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ product_id: product.id, quantity: 1 }),
      })
      if (!response.ok) throw new Error(await readError(response))
      applyCartSnapshot(await response.json() as Cart)
      setNotice(`Đã thêm “${product.name}” vào giỏ.`)
      window.setTimeout(() => setNotice(''), 3500)
    } catch (error) {
      setStockRefresh((value) => value + 1)
      setNotice(error instanceof Error ? error.message : 'Không thể thêm sản phẩm vào giỏ.')
      window.setTimeout(() => setNotice(''), 4000)
    }
  }

  async function updateCartItem(productID: number, quantity: number) {
    try {
      const response = await authorizedFetch(`/api/v1/cart/items/${productID}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ quantity }),
      })
      if (!response.ok) throw new Error(await readError(response))
      applyCartSnapshot(await response.json() as Cart)
    } catch (error) {
      setStockRefresh((value) => value + 1)
      setCheckoutError(error instanceof Error ? error.message : 'Không thể cập nhật giỏ hàng.')
    }
  }

  async function removeCartItem(productID: number) {
    try {
      const response = await authorizedFetch(`/api/v1/cart/items/${productID}`, { method: 'DELETE' })
      if (!response.ok) {
        const message = await readError(response)
        if (response.status === 409) {
          const currentCart = await authorizedFetch('/api/v1/cart')
          if (currentCart.ok) applyCartSnapshot(await currentCart.json() as Cart)
        }
        throw new Error(message)
      }
      setCart(await response.json() as Cart)
      setStockRefresh((value) => value + 1)
      setCheckoutError('')
    } catch (error) {
      setCheckoutError(error instanceof Error ? error.message : 'Không thể xóa sản phẩm khỏi giỏ.')
    }
  }

  async function submitCheckout(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCheckoutBusy(true)
    setCheckoutError('')
    const values = new FormData(event.currentTarget)
    try {
      const response = await authorizedFetch('/api/v1/checkout', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': checkoutKeyRef.current || (checkoutKeyRef.current = crypto.randomUUID()) },
        body: JSON.stringify({
          recipient_name: String(values.get('recipient_name') ?? '').trim(),
          phone: String(values.get('phone') ?? '').trim(),
          address: String(values.get('address') ?? '').trim(),
          note: String(values.get('note') ?? '').trim(),
          payment_method: 'cod',
        }),
      })
      if (!response.ok) {
        const message = await readError(response)
        if (response.status === 409) {
          const currentCart = await authorizedFetch('/api/v1/cart')
          if (currentCart.ok) applyCartSnapshot(await currentCart.json() as Cart)
        }
        throw new Error(message)
      }
      const result = (await response.json()) as { data: Order }
      checkoutKeyRef.current = ''
      setCart(emptyCart)
      setStockRefresh((value) => value + 1)
      setCheckoutOpen(false)
      setCartOpen(false)
      setSelectedOrder(result.data)
      setSelectedOrderAdmin(false)
      setOrderReload((value) => value + 1)
      setNotice(`Đặt hàng thành công · ${result.data.code}`)
      window.setTimeout(() => setNotice(''), 5000)
    } catch (error) {
      setCheckoutError(error instanceof Error ? error.message : 'Không thể hoàn tất đơn hàng.')
    } finally {
      setCheckoutBusy(false)
    }
  }

  function applyCartSnapshot(nextCart: Cart) {
    setCart(nextCart)
    setProductsData((current) => ({
      ...current,
      data: current.data.map((product) => {
        const cartItem = nextCart.items.find((item) => item.product_id === product.id)
        return cartItem ? { ...product, available_stock: cartItem.stock, reserved_stock: Math.max(0, product.stock - cartItem.stock) } : product
      }),
    }))
    setStockRefresh((value) => value + 1)
  }

  async function openOrderDetails(order: Order, admin = false) {
    setOrdersOpen(false)
    setSelectedOrder(order)
    setSelectedOrderAdmin(admin)
    try {
      const response = await authorizedFetch(`/api/v1/${admin ? 'admin/' : ''}orders/${order.id}`)
      if (!response.ok) throw new Error(await readError(response))
      const result = (await response.json()) as { data: Order }
      setSelectedOrder(result.data)
    } catch (error) {
      setOrderError(error instanceof Error ? error.message : 'Không tải được chi tiết đơn hàng.')
    }
  }

  async function cancelOrder(order: Order) {
    if (!window.confirm(`Hủy đơn ${order.code}? Tồn kho sẽ được hoàn lại.`)) return
    try {
      const response = await authorizedFetch(`/api/v1/orders/${order.id}/cancel`, { method: 'POST' })
      if (!response.ok) throw new Error(await readError(response))
      const result = (await response.json()) as { data: Order }
      setSelectedOrder(result.data)
      setOrderReload((value) => value + 1)
      setNotice('Đơn hàng đã được hủy.')
      window.setTimeout(() => setNotice(''), 4000)
    } catch (error) {
      setOrderError(error instanceof Error ? error.message : 'Không thể hủy đơn hàng.')
    }
  }

  async function advanceOrder(order: Order, status: OrderStatus) {
    try {
      const response = await authorizedFetch(`/api/v1/admin/orders/${order.id}/status`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status }),
      })
      if (!response.ok) throw new Error(await readError(response))
      const result = (await response.json()) as { data: Order }
      setAdminOrderReload((value) => value + 1)
      if (selectedOrder?.id === order.id) setSelectedOrder(result.data)
      setOrderError('')
    } catch (error) {
      setOrderError(error instanceof Error ? error.message : 'Không thể cập nhật trạng thái đơn.')
    }
  }

  async function startConversation(orderID?: number) {
    setChatError('')
    try {
      const response = await authorizedFetch('/api/v1/chat/conversations', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(orderID ? { order_id: orderID } : {}),
      })
      if (!response.ok) throw new Error(await readError(response))
      const result = await response.json() as { id: number }
      setChatConversationID(result.id)
      setChatOpen(true)
      setSelectedOrder(null)
      setChatMessages([])
      setChatConversations((current) => [{ id: result.id, owner_id: session?.user.id ?? 0, owner_name: session?.user.name ?? '', order_id: orderID, last_message_at: new Date().toISOString(), created_at: new Date().toISOString() }, ...current])
    } catch (error) {
      setChatError(error instanceof Error ? error.message : 'Không thể bắt đầu hội thoại.')
    }
  }

  async function sendChatMessage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!chatConversationID || !chatDraft.trim()) return
    setChatBusy(true)
    setChatError('')
    try {
      const response = await authorizedFetch(`/api/v1/chat/conversations/${chatConversationID}/messages`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: chatDraft, client_message_id: crypto.randomUUID() }),
      })
      if (!response.ok) throw new Error(await readError(response))
      const result = await response.json() as { data: ChatMessage }
      setChatMessages((current) => current.some((message) => message.id === result.data.id) ? current : [...current, result.data])
      setChatConversations((current) => current.map((item) => item.id === chatConversationID ? { ...item, last_message: result.data.body, last_message_at: result.data.created_at } : item).sort((a, b) => b.last_message_at.localeCompare(a.last_message_at)))
      setChatDraft('')
    } catch (error) {
      setChatError(error instanceof Error ? error.message : 'Không gửi được tin nhắn.')
    } finally {
      setChatBusy(false)
    }
  }

  async function loadOlderChatMessages() {
    if (!chatConversationID || !chatMessages.length) return
    try {
      const response = await authorizedFetch(`/api/v1/chat/conversations/${chatConversationID}/messages?limit=50&before_id=${chatMessages[0].id}`)
      if (!response.ok) throw new Error(await readError(response))
      const result = await response.json() as { data: ChatMessage[] }
      setChatMessages((current) => [...result.data.filter((item) => !current.some((existing) => existing.id === item.id)), ...current])
    } catch (error) {
      setChatError(error instanceof Error ? error.message : 'Không tải được tin nhắn cũ hơn.')
    }
  }

  return (
    <main>
      <header className="topbar">
        <a className="brand" href="/">local<span>market</span></a>
        <nav><a href="#categories">Danh mục</a><a href="#products">Sản phẩm</a><a href="#contact">Liên hệ</a></nav>
        <div className="account-actions">
          {session ? (
            <>
              <span className="account-name">{session.user.name}<small className={`role-badge ${session.user.role}`}>{session.user.role === 'admin' ? 'Admin' : 'User'}</small></span>
              <button className="auth-outline-button" type="button" onClick={() => { setOrdersOpen(true); setOrderError('') }}>Đơn hàng</button>
              <button className="auth-outline-button" type="button" onClick={() => { setChatOpen(true); setChatError('') }}>Tin nhắn</button>
              {session.user.role === 'admin' && <button className="auth-outline-button" type="button" onClick={() => { setAdminOpen((open) => !open); setAdminError('') }}>{adminOpen ? 'Đóng quản trị' : 'Quản trị'}</button>}
              <button className="auth-outline-button" type="button" onClick={logout}>Đăng xuất</button>
            </>
          ) : (
            <>
              <button className="auth-text-button" type="button" onClick={() => openAuth('login')}>Đăng nhập</button>
              <button className="auth-primary-button" type="button" onClick={() => openAuth('register')}>Đăng ký</button>
            </>
          )}
          <button className="cart-button" type="button" onClick={() => { if (!session) openAuth('login'); else { setCartOpen(true); setCheckoutError('') } }}>Giỏ hàng <span>{cart.item_count}</span></button>
        </div>
      </header>

      <section className="hero">
        <div className="hero-copy">
          <p className="eyebrow">TINH HOA ĐỊA PHƯƠNG</p>
          <h1>Mang hương vị quê nhà đến gần bạn hơn.</h1>
          <p className="hero-description">Khám phá đặc sản, sản phẩm thủ công và những câu chuyện tuyệt vời từ các vùng miền Việt Nam.</p>
          <a className="primary-button" href="#products">Khám phá sản phẩm <span>→</span></a>
        </div>
        <div className="hero-art" aria-label="Minh họa sản phẩm địa phương">
          <div className="art-circle">🌿</div><div className="art-product">🧺</div><div className="art-leaf">✦</div>
        </div>
      </section>

      {session?.user.role === 'admin' && adminOpen && (
        <section className="admin-panel section" aria-label="Quản trị danh mục và sản phẩm">
          <div className="section-heading"><div><p className="eyebrow">QUẢN TRỊ</p><h2>Danh mục & sản phẩm</h2></div><button className="auth-primary-button" type="button" onClick={() => { setProductEditor('new'); setUploadedImage(''); setImagePreview(''); setAdminError('') }}>+ Thêm sản phẩm</button></div>
          <div className="admin-section-heading"><h3>Danh mục</h3><button className="auth-outline-button" type="button" onClick={() => { setCategoryEditor('new'); setAdminError('') }}>+ Thêm danh mục</button></div>
          {categoriesData.length ? <div className="category-admin-list">
            {categoriesData.map((category) => <div className="category-admin-row" key={category.id}><div><strong>{category.name}</strong><small>{category.slug}{category.description ? ` · ${category.description}` : ''}</small></div><div className="admin-row-actions"><button type="button" onClick={() => { setCategoryEditor(category); setAdminError('') }}>Sửa</button><button className="danger-button" type="button" onClick={() => deleteCategory(category)}>Xóa</button></div></div>)}
          </div> : <p className="admin-hint">Chưa có danh mục. Hãy tạo danh mục trước khi thêm sản phẩm.</p>}

          <div className="admin-section-heading product-admin-heading"><h3>Sản phẩm</h3><span>{adminProducts.total} sản phẩm</span></div>
          {adminError && !productEditor && !categoryEditor && <p className="admin-error" role="alert">{adminError}</p>}
          {adminProducts.data.length ? <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Sản phẩm</th><th>Danh mục</th><th>Giá</th><th>Tồn kho</th><th>Trạng thái</th><th></th></tr></thead><tbody>
            {adminProducts.data.map((product) => <tr key={product.id}><td><div className="admin-product-name">{product.image_url ? <img src={product.image_url} alt="" /> : <span>🧺</span>}<strong>{product.name}</strong></div></td><td>{product.category}</td><td>{product.price.toLocaleString('vi-VN')}đ</td><td>{product.stock}<small className="table-subtext">Khả dụng {product.available_stock}{product.reserved_stock ? ` · giữ ${product.reserved_stock}` : ''}</small></td><td><span className={`product-status ${product.is_active ? 'active' : 'inactive'}`}>{product.is_active ? 'Đang bán' : 'Ẩn'}</span></td><td><div className="admin-row-actions"><button type="button" onClick={() => { setProductEditor(product); setUploadedImage(product.image_url); setImagePreview(product.image_url); setAdminError('') }}>Sửa</button><button className="danger-button" type="button" onClick={() => deleteProduct(product)}>Xóa</button></div></td></tr>)}
          </tbody></table></div> : <p className="admin-hint">Chưa có sản phẩm phù hợp.</p>}
          <div className="pagination admin-pagination"><button type="button" disabled={adminPage <= 1} onClick={() => setAdminPage((value) => value - 1)}>← Trước</button><span>Trang {adminPage} / {Math.max(1, Math.ceil(adminProducts.total / 8))}</span><button type="button" disabled={adminPage >= Math.ceil(adminProducts.total / 8)} onClick={() => setAdminPage((value) => value + 1)}>Sau →</button></div>

          <div className="admin-section-heading product-admin-heading"><div><h3>Đơn hàng</h3><span>{adminOrderPage.total} đơn</span></div></div>
          <form className="catalog-toolbar order-toolbar" onSubmit={(event) => { event.preventDefault(); setAdminOrderPageNumber(1); setAdminOrderSearch(adminOrderSearchInput.trim()) }}>
            <label className="catalog-search"><span>⌕</span><input value={adminOrderSearchInput} onChange={(event) => setAdminOrderSearchInput(event.target.value)} placeholder="Tìm mã đơn, email, người nhận..." aria-label="Tìm đơn hàng" /></label>
            <select value={adminOrderStatus} onChange={(event) => { setAdminOrderStatus(event.target.value); setAdminOrderPageNumber(1) }} aria-label="Lọc trạng thái"><option value="">Tất cả trạng thái</option><option value="confirmed">Đã xác nhận</option><option value="processing">Đang chuẩn bị</option><option value="shipped">Đang giao</option><option value="delivered">Đã giao</option><option value="cancelled">Đã hủy</option></select>
            <button className="catalog-search-button" type="submit">Tìm đơn</button>
          </form>
          {orderError && <p className="admin-error" role="alert">{orderError}</p>}
          {adminOrderPage.data.length ? <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>Mã đơn</th><th>Người nhận</th><th>Ngày đặt</th><th>Tổng tiền</th><th>Trạng thái</th><th>Thao tác</th></tr></thead><tbody>
            {adminOrderPage.data.map((order) => <tr key={order.id}><td><button className="table-link" type="button" onClick={() => openOrderDetails(order, true)}>{order.code}</button></td><td>{order.recipient_name}<small className="table-subtext">{order.phone}</small></td><td>{new Date(order.created_at).toLocaleDateString('vi-VN')}</td><td>{formatMoney(order.total)}</td><td><span className={`product-status ${order.status === 'cancelled' ? 'inactive' : 'active'}`}>{orderStatusLabel(order.status)}</span></td><td><div className="admin-row-actions">{order.status === 'confirmed' && <><button type="button" onClick={() => advanceOrder(order, 'processing')}>Chuẩn bị</button><button className="danger-button" type="button" onClick={() => advanceOrder(order, 'cancelled')}>Hủy</button></>}{order.status === 'processing' && <><button type="button" onClick={() => advanceOrder(order, 'shipped')}>Gửi hàng</button><button className="danger-button" type="button" onClick={() => advanceOrder(order, 'cancelled')}>Hủy</button></>}{order.status === 'shipped' && <button type="button" onClick={() => advanceOrder(order, 'delivered')}>Đã giao</button>}</div></td></tr>)}
          </tbody></table></div> : <p className="admin-hint">Chưa có đơn hàng phù hợp.</p>}
          <div className="pagination admin-pagination"><button type="button" disabled={adminOrderPageNumber <= 1} onClick={() => setAdminOrderPageNumber((value) => value - 1)}>← Trước</button><span>Trang {adminOrderPageNumber} / {Math.max(1, Math.ceil(adminOrderPage.total / 8))}</span><button type="button" disabled={adminOrderPageNumber >= Math.ceil(adminOrderPage.total / 8)} onClick={() => setAdminOrderPageNumber((value) => value + 1)}>Sau →</button></div>
        </section>
      )}

      <section id="categories" className="section">
        <div className="section-heading"><div><p className="eyebrow">KHÁM PHÁ</p><h2>Danh mục nổi bật</h2></div><a href="#products">Xem tất cả →</a></div>
        <div className="category-grid">
          {categoriesData.map((category, index) => <button className={`category-card ${categoryFilter === String(category.id) ? 'selected' : ''}`} type="button" onClick={() => { setCategoryFilter(categoryFilter === String(category.id) ? '' : String(category.id)); setProductPage(1) }} key={category.id}><span>{['🍯', '🥮', '🧵', '🎁'][index % 4]}</span><strong>{category.name}</strong></button>)}
          {!categoriesData.length && <p className="empty-catalog">Danh mục sản phẩm sẽ sớm được cập nhật.</p>}
        </div>
      </section>

      <section id="products" className="section products-section">
        <div className="section-heading"><div><p className="eyebrow">ĐƯỢC TUYỂN CHỌN</p><h2>Sản phẩm địa phương</h2></div><span className="muted">{productsData.total} sản phẩm</span></div>
        <form className="catalog-toolbar" onSubmit={(event) => { event.preventDefault(); setProductPage(1); setSearchQuery(searchInput.trim()) }}>
          <label className="catalog-search"><span>⌕</span><input value={searchInput} onChange={(event) => setSearchInput(event.target.value)} placeholder="Tìm sản phẩm, đặc sản..." aria-label="Tìm kiếm sản phẩm" /></label>
          <select value={categoryFilter} onChange={(event) => { setCategoryFilter(event.target.value); setProductPage(1) }} aria-label="Lọc theo danh mục"><option value="">Tất cả danh mục</option>{categoriesData.map((category) => <option key={category.id} value={category.id}>{category.name}</option>)}</select>
          <select value={sortBy} onChange={(event) => { setSortBy(event.target.value); setProductPage(1) }} aria-label="Sắp xếp sản phẩm"><option value="created_at">Mới nhất</option><option value="name">Tên A–Z</option><option value="price">Giá tăng dần</option></select>
          <button className="catalog-search-button" type="submit">Tìm kiếm</button>
        </form>
        {catalogError && <p className="catalog-error" role="alert">{catalogError}</p>}
        {productsLoading ? <p className="empty-catalog">Đang tải sản phẩm...</p> : productsData.data.length ? <div className="product-grid">
          {productsData.data.map((product) => <article className="product-card" key={product.id}>
            <div className="product-art">{product.image_url ? <img src={product.image_url} alt={product.name} /> : <span>🧺</span>}<small>{product.category}</small></div>
            <div className="product-info"><p>{product.category}</p><h3>{product.name}</h3><p className="product-description">{product.description}</p><div className="product-bottom"><div className="product-price-stock"><strong>{formatMoney(product.price)}</strong><span className="stock-label">Còn {product.available_stock} khả dụng</span></div><button className={`cart-add-button${product.available_stock < 1 ? ' sold-out' : ''}`} type="button" aria-label={product.available_stock < 1 ? `${product.name} đã hết hàng` : `Thêm ${product.name} vào giỏ hàng`} title={product.available_stock < 1 ? 'Tạm hết hàng' : 'Thêm vào giỏ hàng'} disabled={product.available_stock < 1} onClick={() => addCartItem(product)}>{product.available_stock < 1 ? 'Hết hàng' : <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M3 3h2l2.4 11.4a2 2 0 0 0 2 1.6h7.9a2 2 0 0 0 1.9-1.5L21 8H6" /><circle cx="10" cy="20" r="1" /><circle cx="18" cy="20" r="1" /><path d="M14 4v6m-3-3h6" /></svg>}</button></div></div>
          </article>)}
        </div> : <p className="empty-catalog">{searchQuery || categoryFilter ? 'Không tìm thấy sản phẩm phù hợp.' : 'Chưa có sản phẩm nào. Quay lại sau nhé!'}</p>}
        <div className="pagination"><button type="button" disabled={productPage <= 1} onClick={() => setProductPage((value) => value - 1)}>← Trước</button><span>Trang {productPage} / {Math.max(1, Math.ceil(productsData.total / 8))}</span><button type="button" disabled={productPage >= Math.ceil(productsData.total / 8)} onClick={() => setProductPage((value) => value + 1)}>Sau →</button></div>
      </section>

      <section id="contact" className="contact-strip"><div><p className="eyebrow">KẾT NỐI VỚI CHÚNG TÔI</p><h2>Bạn muốn tìm hiểu thêm về sản phẩm?</h2><p>Đội ngũ Local Market luôn sẵn sàng hỗ trợ bạn.</p></div><a className="primary-button" href="mailto:hello@example.com">Liên hệ ngay</a></section>

      <footer><a className="brand" href="/">local<span>market</span></a><p>Khám phá và trân trọng giá trị địa phương.</p><small>© 2026 Local Market — Development preview</small></footer>

      <div className="system-status">
        <span className={health && !healthError ? 'status-dot online' : 'status-dot'} />
        {healthError ? 'Không kết nối được API' : health ? `API hoạt động · PostgreSQL ${health.dependencies.postgres} · Redis ${health.dependencies.redis}` : 'Đang kiểm tra kết nối...'}
      </div>

      {cartOpen && session && (
        <div className="auth-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setCartOpen(false) }}>
          <section className="auth-dialog shopping-dialog" role="dialog" aria-modal="true" aria-labelledby="cart-title">
            <button className="auth-close" type="button" aria-label="Đóng giỏ hàng" onClick={() => setCartOpen(false)}>×</button>
            <p className="eyebrow">GIỎ HÀNG CỦA BẠN</p><h2 id="cart-title">Giỏ hàng <span className="cart-count-label">{cart.item_count} sản phẩm</span></h2>
            {cart.items.length ? <div className="cart-line-list">{cart.items.map((item) => <div className="cart-line" key={item.product_id}>
              {item.image_url ? <img src={item.image_url} alt="" /> : <span className="cart-line-placeholder">🧺</span>}
              <div className="cart-line-info"><strong>{item.name}</strong><span>{formatMoney(item.price)}</span><small>Đang giữ {item.quantity} · còn {item.stock} khả dụng thêm</small></div>
              <div className="quantity-control"><button type="button" aria-label="Giảm số lượng" disabled={item.quantity <= 1} onClick={() => updateCartItem(item.product_id, item.quantity - 1)}>−</button><span>{item.quantity}</span><button type="button" aria-label="Tăng số lượng" disabled={item.stock < 1} onClick={() => updateCartItem(item.product_id, item.quantity + 1)}>+</button></div>
              <button className="remove-line" type="button" onClick={() => removeCartItem(item.product_id)}>Xóa</button>
            </div>)}</div> : <p className="empty-catalog cart-empty">Giỏ hàng đang trống. Hãy chọn vài sản phẩm bạn thích nhé.</p>}
            {checkoutError && <p className="auth-error" role="alert">{checkoutError}</p>}
            <div className="cart-total-row"><span>Tạm tính</span><strong>{formatMoney(cart.total)}</strong></div>
            <button className="auth-submit" type="button" disabled={!cart.items.length} onClick={() => { setCartOpen(false); setCheckoutOpen(true); setCheckoutError('') }}>Tiến hành đặt hàng</button>
          </section>
        </div>
      )}

      {checkoutOpen && session && (
        <div className="auth-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setCheckoutOpen(false) }}>
          <section className="auth-dialog shopping-dialog" role="dialog" aria-modal="true" aria-labelledby="checkout-title">
            <button className="auth-close" type="button" aria-label="Đóng thanh toán" onClick={() => setCheckoutOpen(false)}>×</button>
            <p className="eyebrow">HOÀN TẤT ĐƠN HÀNG</p><h2 id="checkout-title">Thông tin giao hàng</h2>
            <p className="auth-subtitle">Đơn hàng có {cart.item_count} sản phẩm · Tổng {formatMoney(cart.total)}</p>
            <form className="auth-form admin-form" onSubmit={submitCheckout}>
              <label>Người nhận<input name="recipient_name" autoComplete="name" defaultValue={session.user.name} maxLength={120} required /></label>
              <label>Số điện thoại<input name="phone" type="tel" autoComplete="tel" maxLength={40} required /></label>
              <label>Địa chỉ giao hàng<textarea name="address" autoComplete="street-address" rows={3} maxLength={1000} required /></label>
              <label>Ghi chú<textarea name="note" rows={2} maxLength={1000} placeholder="Không bắt buộc" /></label>
              <div className="payment-method-note"><span>💵</span><div><strong>Thanh toán khi nhận hàng (COD)</strong><small>Thanh toán cho đơn vị giao hàng khi nhận sản phẩm.</small></div></div>
              {checkoutError && <p className="auth-error" role="alert">{checkoutError}</p>}
              <button className="auth-submit" type="submit" disabled={checkoutBusy || !cart.items.length}>{checkoutBusy ? 'Đang tạo đơn...' : `Đặt hàng · ${formatMoney(cart.total)}`}</button>
            </form>
          </section>
        </div>
      )}

      {ordersOpen && session && (
        <div className="auth-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setOrdersOpen(false) }}>
          <section className="auth-dialog orders-dialog" role="dialog" aria-modal="true" aria-labelledby="orders-title">
            <button className="auth-close" type="button" aria-label="Đóng đơn hàng" onClick={() => setOrdersOpen(false)}>×</button>
            <p className="eyebrow">LỊCH SỬ MUA HÀNG</p><h2 id="orders-title">Đơn hàng của tôi</h2>
            {orderError && <p className="auth-error" role="alert">{orderError}</p>}
            {orderPage.data.length ? <div className="order-card-list">{orderPage.data.map((order) => <button className="order-card" type="button" key={order.id} onClick={() => openOrderDetails(order)}>
              <span className="order-card-top"><strong>{order.code}</strong><small className={`product-status ${order.status === 'cancelled' ? 'inactive' : 'active'}`}>{orderStatusLabel(order.status)}</small></span>
              <span className="order-card-bottom"><small>{new Date(order.created_at).toLocaleString('vi-VN')}</small><strong>{formatMoney(order.total)}</strong></span>
            </button>)}</div> : <p className="empty-catalog">Bạn chưa có đơn hàng nào.</p>}
            <div className="pagination"><button type="button" disabled={orderPageNumber <= 1} onClick={() => setOrderPageNumber((value) => value - 1)}>← Trước</button><span>Trang {orderPageNumber} / {Math.max(1, Math.ceil(orderPage.total / 8))}</span><button type="button" disabled={orderPageNumber >= Math.ceil(orderPage.total / 8)} onClick={() => setOrderPageNumber((value) => value + 1)}>Sau →</button></div>
          </section>
        </div>
      )}

      {selectedOrder && (
        <div className="auth-overlay order-detail-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setSelectedOrder(null) }}>
          <section className="auth-dialog orders-dialog" role="dialog" aria-modal="true" aria-labelledby="order-detail-title">
            <button className="auth-close" type="button" aria-label="Đóng chi tiết đơn" onClick={() => setSelectedOrder(null)}>×</button>
            <p className="eyebrow">CHI TIẾT ĐƠN HÀNG</p><h2 id="order-detail-title">{selectedOrder.code}</h2>
            <div className="order-detail-meta"><span className={`product-status ${selectedOrder.status === 'cancelled' ? 'inactive' : 'active'}`}>{orderStatusLabel(selectedOrder.status)}</span><small>{new Date(selectedOrder.created_at).toLocaleString('vi-VN')}</small></div>
            <div className="order-address"><strong>{selectedOrder.recipient_name} · {selectedOrder.phone}</strong><span>{selectedOrder.address}</span><small>Thanh toán COD · {selectedOrder.payment_status === 'paid' ? 'Đã thanh toán' : selectedOrder.payment_status === 'cancelled' ? 'Đã hủy' : 'Chưa thanh toán'}</small></div>
            <div className="order-item-list">{selectedOrder.items?.map((item) => <div className="order-item" key={item.id}>{item.image_url ? <img src={item.image_url} alt="" /> : <span className="cart-line-placeholder">🧺</span>}<span>{item.product_name} <small>× {item.quantity}</small></span><strong>{formatMoney(item.line_total)}</strong></div>)}</div>
            <div className="cart-total-row"><span>Tổng cộng</span><strong>{formatMoney(selectedOrder.total)}</strong></div>
            {selectedOrder.status_history?.length ? <div className="order-timeline"><strong>Lịch sử trạng thái</strong>{selectedOrder.status_history.map((change, index) => <div key={`${change.created_at}-${index}`}><span className="timeline-dot" /><span>{orderStatusLabel(change.to_status)}<small>{new Date(change.created_at).toLocaleString('vi-VN')}</small></span></div>)}</div> : null}
            {orderError && <p className="auth-error" role="alert">{orderError}</p>}
            {!selectedOrderAdmin && <button className="auth-submit chat-order-button" type="button" onClick={() => startConversation(selectedOrder.id)}>Liên hệ hỗ trợ về đơn hàng này</button>}
            {!selectedOrderAdmin && selectedOrder.status === 'confirmed' && <button className="danger-wide-button" type="button" onClick={() => cancelOrder(selectedOrder)}>Hủy đơn hàng</button>}
          </section>
        </div>
      )}

      {chatOpen && session && (
        <div className="auth-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setChatOpen(false) }}>
          <section className="auth-dialog chat-dialog" role="dialog" aria-modal="true" aria-labelledby="chat-title">
            <button className="auth-close" type="button" aria-label="Đóng tin nhắn" onClick={() => setChatOpen(false)}>×</button>
            <p className="eyebrow">HỖ TRỢ KHÁCH HÀNG</p><h2 id="chat-title">Tin nhắn</h2>
            <div className="chat-layout">
              <aside className="chat-sidebar">
                {chatConversations.map((conversation) => <button className={`chat-thread${chatConversationID === conversation.id ? ' active' : ''}`} type="button" key={conversation.id} onClick={() => { setChatConversationID(conversation.id); setChatError('') }}>
                  <strong>{session.user.role === 'admin' ? conversation.owner_name : conversation.order_id ? `Đơn hàng #${conversation.order_id}` : conversation.product_name || 'Hỗ trợ Local Market'}</strong>
                  <small>{conversation.last_message || 'Chưa có tin nhắn'}</small>
                </button>)}
                {session.user.role !== 'admin' && <button className="auth-outline-button chat-new-button" type="button" onClick={() => startConversation()}>+ Bắt đầu hội thoại</button>}
                {!chatConversations.length && session.user.role === 'admin' && <p className="chat-empty">Chưa có hội thoại mới.</p>}
              </aside>
              <div className="chat-main">
                {chatConversationID ? <>
                  <div className="chat-status"><span className={chatConnected ? 'connected' : ''} />{chatConnected ? 'Đang kết nối realtime' : 'Đang đồng bộ lịch sử'}</div>
                  <div className="chat-message-list" ref={chatListRef}>
                    {chatMessages.length >= 50 && <button className="chat-older-button" type="button" onClick={loadOlderChatMessages}>Tải tin nhắn cũ hơn</button>}
                    {chatMessages.map((message) => <div className={`chat-message${message.sender_id === session.user.id ? ' own' : ''}`} key={message.id}>
                      <small>{message.sender_name} · {new Date(message.created_at).toLocaleString('vi-VN')}</small><p>{message.body}</p>
                    </div>)}
                    {!chatMessages.length && <p className="chat-empty">Gửi lời nhắn đầu tiên để bắt đầu trao đổi.</p>}
                  </div>
                  <form className="chat-compose" onSubmit={sendChatMessage}>
                    <input value={chatDraft} onChange={(event) => setChatDraft(event.target.value)} maxLength={4000} placeholder="Nhập tin nhắn..." aria-label="Nội dung tin nhắn" />
                    <button type="submit" disabled={chatBusy || !chatDraft.trim()}>{chatBusy ? '...' : 'Gửi'}</button>
                  </form>
                </> : <div className="chat-no-selection">{session.user.role === 'admin' ? 'Chọn hội thoại để xem và trả lời.' : 'Bắt đầu hội thoại để được hỗ trợ.'}</div>}
              </div>
            </div>
            {chatError && <p className="auth-error chat-error" role="alert">{chatError}</p>}
          </section>
        </div>
      )}

      {notice && <div className="auth-toast" role="status">{notice}</div>}

      {authOpen && (
        <div className="auth-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setAuthOpen(false) }}>
          <section className="auth-dialog" role="dialog" aria-modal="true" aria-labelledby="auth-title">
            <button className="auth-close" type="button" aria-label="Đóng" onClick={() => setAuthOpen(false)}>×</button>
            <a className="brand auth-brand" href="/">local<span>market</span></a>
            <p className="eyebrow">CHÀO MỪNG BẠN</p>
            <h2 id="auth-title">{authMode === 'login' ? 'Đăng nhập' : 'Tạo tài khoản'}</h2>
            <p className="auth-subtitle">{authMode === 'login' ? 'Tiếp tục khám phá những sản phẩm địa phương.' : 'Tạo tài khoản để bắt đầu hành trình của bạn.'}</p>

            <div className="auth-tabs" role="tablist" aria-label="Chọn hình thức">
              <button type="button" role="tab" aria-selected={authMode === 'login'} className={authMode === 'login' ? 'active' : ''} onClick={() => { setAuthMode('login'); setAuthError('') }}>Đăng nhập</button>
              <button type="button" role="tab" aria-selected={authMode === 'register'} className={authMode === 'register' ? 'active' : ''} onClick={() => { setAuthMode('register'); setAuthError('') }}>Đăng ký</button>
            </div>

            <form className="auth-form" onSubmit={submitAuth}>
              {authMode === 'register' && <label>Họ và tên<input name="name" type="text" autoComplete="name" placeholder="Tên của bạn" maxLength={100} required /></label>}
              <label>Email<input name="email" type="email" autoComplete="email" placeholder="ban@example.com" maxLength={254} required /></label>
              <label>Mật khẩu<input name="password" type="password" autoComplete={authMode === 'login' ? 'current-password' : 'new-password'} placeholder="Ít nhất 8 ký tự" minLength={8} maxLength={72} required /></label>
              {authError && <p className="auth-error" role="alert">{authError}</p>}
              <button className="auth-submit" type="submit" disabled={authBusy}>{authBusy ? 'Đang xử lý...' : authMode === 'login' ? 'Đăng nhập' : 'Tạo tài khoản'}</button>
            </form>

            <p className="auth-switch">{authMode === 'login' ? 'Bạn chưa có tài khoản?' : 'Bạn đã có tài khoản?'}{' '}
              <button type="button" onClick={() => { setAuthMode(authMode === 'login' ? 'register' : 'login'); setAuthError('') }}>{authMode === 'login' ? 'Đăng ký ngay' : 'Đăng nhập'}</button>
            </p>
          </section>
        </div>
      )}

      {categoryEditor && (
        <div className="auth-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setCategoryEditor(null) }}>
          <section className="auth-dialog admin-dialog" role="dialog" aria-modal="true" aria-labelledby="category-dialog-title">
            <button className="auth-close" type="button" aria-label="Đóng" onClick={() => setCategoryEditor(null)}>×</button>
            <p className="eyebrow">QUẢN TRỊ DANH MỤC</p>
            <h2 id="category-dialog-title">{categoryEditor === 'new' ? 'Thêm danh mục' : 'Sửa danh mục'}</h2>
            <form className="auth-form admin-form" onSubmit={submitCategory}>
              <label>Tên danh mục<input name="name" defaultValue={categoryEditor === 'new' ? '' : categoryEditor.name} maxLength={120} required /></label>
              <label>Slug<input name="slug" defaultValue={categoryEditor === 'new' ? '' : categoryEditor.slug} placeholder="Tự tạo từ tên nếu để trống" maxLength={140} /></label>
              <label>Mô tả<textarea name="description" defaultValue={categoryEditor === 'new' ? '' : categoryEditor.description} rows={3} /></label>
              {adminError && <p className="auth-error" role="alert">{adminError}</p>}
              <button className="auth-submit" type="submit" disabled={editorBusy}>{editorBusy ? 'Đang lưu...' : 'Lưu danh mục'}</button>
            </form>
          </section>
        </div>
      )}

      {productEditor && (
        <div className="auth-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) { setProductEditor(null); setAdminError('') } }}>
          <section className="auth-dialog admin-dialog product-dialog" role="dialog" aria-modal="true" aria-labelledby="product-dialog-title">
            <button className="auth-close" type="button" aria-label="Đóng" onClick={() => { setProductEditor(null); setAdminError('') }}>×</button>
            <p className="eyebrow">QUẢN TRỊ SẢN PHẨM</p>
            <h2 id="product-dialog-title">{productEditor === 'new' ? 'Thêm sản phẩm' : 'Sửa sản phẩm'}</h2>
            <form className="auth-form admin-form" onSubmit={submitProduct}>
              <label>Tên sản phẩm<input name="name" defaultValue={productEditor === 'new' ? '' : productEditor.name} maxLength={180} required /></label>
              <label>Slug<input name="slug" defaultValue={productEditor === 'new' ? '' : productEditor.slug} placeholder="Tự tạo từ tên nếu để trống" maxLength={200} /></label>
              <label>Danh mục<select name="category_id" defaultValue={productEditor === 'new' ? '' : String(productEditor.category_id)} required><option value="" disabled>Chọn danh mục</option>{categoriesData.map((category) => <option key={category.id} value={category.id}>{category.name}</option>)}</select></label>
              <div className="admin-form-grid"><label>Giá (đ)<input name="price" type="number" min="0" step="1000" defaultValue={productEditor === 'new' ? '' : productEditor.price} required /></label><label>Tồn kho<input name="stock" type="number" min="0" step="1" defaultValue={productEditor === 'new' ? 0 : productEditor.stock} required /></label></div>
              <label>Mô tả<textarea name="description" defaultValue={productEditor === 'new' ? '' : productEditor.description} rows={3} /></label>
              <label>Ảnh sản phẩm<input type="file" accept="image/jpeg,image/png,image/gif" onChange={uploadProductImage} /></label>
              {imageBusy && <p className="admin-hint">Đang tải ảnh lên...</p>}
              {imagePreview && <div className="image-preview"><img src={imagePreview} alt="Xem trước ảnh sản phẩm" /><button type="button" onClick={() => { setUploadedImage(''); setImagePreview('') }}>Gỡ ảnh</button></div>}
              <label className="checkbox-label"><input name="is_active" type="checkbox" defaultChecked={productEditor === 'new' || productEditor.is_active} /> Hiển thị sản phẩm</label>
              {adminError && <p className="auth-error" role="alert">{adminError}</p>}
              <button className="auth-submit" type="submit" disabled={editorBusy || imageBusy || !categoriesData.length}>{editorBusy ? 'Đang lưu...' : 'Lưu sản phẩm'}</button>
            </form>
          </section>
        </div>
      )}
    </main>
  )
}
