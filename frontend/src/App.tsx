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
  image_url: string
  is_active: boolean
}

type ProductPage = { data: Product[]; page: number; limit: number; total: number }

const SESSION_KEY = 'localmarket.auth'

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
  const [categoriesData, setCategoriesData] = useState<Category[]>([])
  const [productsData, setProductsData] = useState<ProductPage>({ data: [], page: 1, limit: 8, total: 0 })
  const [searchInput, setSearchInput] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [categoryFilter, setCategoryFilter] = useState('')
  const [sortBy, setSortBy] = useState('created_at')
  const [productPage, setProductPage] = useState(1)
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
  }, [productPage, searchQuery, categoryFilter, sortBy])

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

  return (
    <main>
      <header className="topbar">
        <a className="brand" href="/">local<span>market</span></a>
        <nav><a href="#categories">Danh mục</a><a href="#products">Sản phẩm</a><a href="#contact">Liên hệ</a></nav>
        <div className="account-actions">
          {session ? (
            <>
              <span className="account-name">{session.user.name}<small className={`role-badge ${session.user.role}`}>{session.user.role === 'admin' ? 'Admin' : 'User'}</small></span>
              {session.user.role === 'admin' && <button className="auth-outline-button" type="button" onClick={() => { setAdminOpen((open) => !open); setAdminError('') }}>{adminOpen ? 'Đóng quản trị' : 'Quản trị'}</button>}
              <button className="auth-outline-button" type="button" onClick={logout}>Đăng xuất</button>
            </>
          ) : (
            <>
              <button className="auth-text-button" type="button" onClick={() => openAuth('login')}>Đăng nhập</button>
              <button className="auth-primary-button" type="button" onClick={() => openAuth('register')}>Đăng ký</button>
            </>
          )}
          <button className="cart-button" type="button" onClick={() => alert('Giỏ hàng sẽ được triển khai ở bước tiếp theo.')}>Giỏ hàng <span>0</span></button>
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
            {adminProducts.data.map((product) => <tr key={product.id}><td><div className="admin-product-name">{product.image_url ? <img src={product.image_url} alt="" /> : <span>🧺</span>}<strong>{product.name}</strong></div></td><td>{product.category}</td><td>{product.price.toLocaleString('vi-VN')}đ</td><td>{product.stock}</td><td><span className={`product-status ${product.is_active ? 'active' : 'inactive'}`}>{product.is_active ? 'Đang bán' : 'Ẩn'}</span></td><td><div className="admin-row-actions"><button type="button" onClick={() => { setProductEditor(product); setUploadedImage(product.image_url); setImagePreview(product.image_url); setAdminError('') }}>Sửa</button><button className="danger-button" type="button" onClick={() => deleteProduct(product)}>Xóa</button></div></td></tr>)}
          </tbody></table></div> : <p className="admin-hint">Chưa có sản phẩm phù hợp.</p>}
          <div className="pagination admin-pagination"><button type="button" disabled={adminPage <= 1} onClick={() => setAdminPage((value) => value - 1)}>← Trước</button><span>Trang {adminPage} / {Math.max(1, Math.ceil(adminProducts.total / 8))}</span><button type="button" disabled={adminPage >= Math.ceil(adminProducts.total / 8)} onClick={() => setAdminPage((value) => value + 1)}>Sau →</button></div>
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
            <div className="product-info"><p>{product.category}</p><h3>{product.name}</h3><p className="product-description">{product.description}</p><div className="product-bottom"><strong>{product.price.toLocaleString('vi-VN')}đ</strong><span className="stock-label">Còn {product.stock}</span></div></div>
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
