const { test, expect } = require('@playwright/test');

// Configuração para simular dispositivo móvel
const mobileViewport = {
  width: 375,
  height: 667,
  deviceScaleFactor: 2,
  isMobile: true,
  hasTouch: true,
};

// iPhone 12 Pro como dispositivo de referência
const iPhone12Pro = {
  userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 14_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Mobile/15E148 Safari/604.1',
  viewport: mobileViewport,
  deviceScaleFactor: 2,
  isMobile: true,
  hasTouch: true,
};

test.describe('Mobile Login Test', () => {
  test.beforeEach(async ({ page }) => {
    // Configurar viewport móvel
    await page.setViewportSize(mobileViewport);
    await page.setExtraHTTPHeaders({
      'User-Agent': iPhone12Pro.userAgent,
    });
  });

  test('deve fazer login com sucesso e sair da tela de login no mobile', async ({ page, baseURL }) => {
    const baseUrl = baseURL || 'http://localhost:5173'; // Vite dev server
    console.log('🔍 Navegando para:', baseUrl);
    
    await page.goto(baseUrl, { waitUntil: 'networkidle' });
    
    // Aguardar a página carregar
    await page.waitForTimeout(1000);
    
    // Verificar se está na tela de login
    console.log('🔍 Verificando se está na tela de login...');
    const loginForm = page.locator('form').first();
    await expect(loginForm).toBeVisible({ timeout: 5000 });
    
    // Preencher credenciais
    const usernameInput = page.locator('input[type="text"], input[name="username"]').first();
    const passwordInput = page.locator('input[type="password"]').first();
    
    console.log('✍️  Preenchendo credenciais...');
    await usernameInput.fill('admin');
    await passwordInput.fill('admin');
    
    // Verificar se os campos foram preenchidos
    await expect(usernameInput).toHaveValue('admin');
    await expect(passwordInput).toHaveValue('admin');
    
    // Interceptar requisições para debug
    page.on('request', (request) => {
      if (request.url().includes('/api/auth/login')) {
        console.log('📤 Request:', request.method(), request.url());
        console.log('📤 Headers:', request.headers());
      }
    });
    
    page.on('response', async (response) => {
      if (response.url().includes('/api/auth/login')) {
        console.log('📥 Response:', response.status(), response.url());
        const body = await response.text();
        console.log('📥 Body:', body.substring(0, 200));
      }
      if (response.url().includes('/api/auth/status')) {
        console.log('📥 Auth Status:', response.status(), response.url());
        const body = await response.text();
        console.log('📥 Status Body:', body.substring(0, 200));
      }
    });
    
    // Clicar no botão de login
    console.log('🔘 Clicando no botão de login...');
    const loginButton = page.locator('button[type="submit"]').first();
    await loginButton.click();
    
    // Aguardar resposta do login
    await page.waitForTimeout(2000);
    
    // Verificar se o token foi armazenado no localStorage
    const token = await page.evaluate(() => {
      return localStorage.getItem('auth_token');
    });
    console.log('🔑 Token no localStorage:', token ? token.substring(0, 20) + '...' : 'NÃO ENCONTRADO');
    
    // Verificar cookies
    const cookies = await page.context().cookies();
    console.log('🍪 Cookies:', cookies.map(c => ({ name: c.name, value: c.value.substring(0, 20) + '...' })));
    
    // Aguardar um pouco mais para o estado atualizar
    await page.waitForTimeout(2000);
    
    // Verificar se saiu da tela de login
    // A tela de login deve desaparecer e mostrar o conteúdo principal
    console.log('🔍 Verificando se saiu da tela de login...');
    
    // Verificar se o formulário de login ainda está visível
    const loginFormStillVisible = await loginForm.isVisible().catch(() => false);
    console.log('📋 Formulário de login ainda visível?', loginFormStillVisible);
    
    // Verificar se há algum elemento da interface principal (sidebar, file browser, etc)
    const sidebar = page.locator('[class*="sidebar"], [class*="Sidebar"]').first();
    const fileBrowser = page.locator('[class*="FileBrowser"], [class*="file-browser"]').first();
    const mainContent = page.locator('main, [role="main"]').first();
    
    const sidebarVisible = await sidebar.isVisible().catch(() => false);
    const fileBrowserVisible = await fileBrowser.isVisible().catch(() => false);
    const mainContentVisible = await mainContent.isVisible().catch(() => false);
    
    console.log('📋 Sidebar visível?', sidebarVisible);
    console.log('📋 FileBrowser visível?', fileBrowserVisible);
    console.log('📋 Main content visível?', mainContentVisible);
    
    // Verificar o estado de autenticação via JavaScript
    const authState = await page.evaluate(() => {
      // Tentar acessar o estado do React via window
      if (window.__REACT_DEVTOOLS_GLOBAL_HOOK__) {
        return 'React DevTools disponível';
      }
      return {
        token: localStorage.getItem('auth_token'),
        hasToken: !!localStorage.getItem('auth_token'),
      };
    });
    console.log('🔍 Estado de autenticação:', authState);
    
    // Fazer uma requisição manual para verificar o status
    const statusResponse = await page.evaluate(async (baseUrl) => {
      const token = localStorage.getItem('auth_token');
      const response = await fetch(`${baseUrl}/api/auth/status`, {
        headers: {
          'Authorization': token ? `Bearer ${token}` : '',
        },
        credentials: 'include',
      });
      return {
        status: response.status,
        body: await response.text(),
      };
    }, baseUrl);
    console.log('📊 Status da API:', statusResponse);
    
    // Assert: O formulário de login NÃO deve estar visível após login bem-sucedido
    // OU algum elemento da interface principal deve estar visível
    if (loginFormStillVisible) {
      console.error('❌ ERRO: Formulário de login ainda está visível após login!');
      console.error('Token:', token ? 'Presente' : 'Ausente');
      console.error('Cookies:', cookies.length);
      console.error('Status API:', statusResponse.status);
      
      // Tirar screenshot para debug
      await page.screenshot({ path: 'test-results/mobile-login-failed.png', fullPage: true });
      
      // Verificar se há mensagens de erro
      const errorMessages = await page.locator('[class*="error"], [class*="Error"]').allTextContents();
      console.error('Mensagens de erro:', errorMessages);
    }
    
    // Verificar se pelo menos um elemento da interface principal está visível
    const mainInterfaceVisible = sidebarVisible || fileBrowserVisible || mainContentVisible;
    
    expect(mainInterfaceVisible || !loginFormStillVisible).toBeTruthy();
  });

  test('deve verificar o fluxo completo de autenticação no mobile', async ({ page, baseURL }) => {
    const baseUrl = baseURL || 'http://localhost:80';
    
    // Try to connect with retries
    let connected = false;
    for (let i = 0; i < 5; i++) {
      try {
        await page.goto(baseUrl, { waitUntil: 'domcontentloaded', timeout: 5000 });
        connected = true;
        break;
      } catch (e) {
        if (i < 4) {
          console.log(`⏳ Tentativa ${i + 1}/5 - aguardando servidor...`);
          await page.waitForTimeout(2000);
        } else {
          throw e;
        }
      }
    }
    await page.waitForTimeout(1000);
    
    // Verificar se está na tela de login inicialmente
    const loginForm = page.locator('form').first();
    await expect(loginForm).toBeVisible({ timeout: 5000 });
    
    // Preencher e submeter login
    await page.locator('input[type="text"], input[name="username"]').first().fill('admin');
    await page.locator('input[type="password"]').first().fill('admin');
    
    // Monitorar requisições
    const requests = [];
    const responses = [];
    
    page.on('request', (request) => {
      if (request.url().includes('/api/')) {
        requests.push({
          url: request.url(),
          method: request.method(),
          headers: request.headers(),
        });
      }
    });
    
    page.on('response', async (response) => {
      if (response.url().includes('/api/')) {
        const body = await response.text().catch(() => '');
        responses.push({
          url: response.url(),
          status: response.status(),
          body: body.substring(0, 500),
        });
      }
    });
    
    // Clicar em login
    await page.locator('button[type="submit"]').first().click();
    
    // Aguardar todas as requisições
    await page.waitForTimeout(3000);
    
    // Verificar token
    const token = await page.evaluate(() => localStorage.getItem('auth_token'));
    
    console.log('📊 Requisições:', requests.length);
    console.log('📊 Respostas:', responses.length);
    console.log('📊 Token:', token ? 'Presente' : 'Ausente');
    
    // Verificar se o token foi armazenado
    expect(token).toBeTruthy();
    
    // Verificar se a requisição de login foi bem-sucedida
    const loginResponse = responses.find(r => r.url.includes('/api/auth/login'));
    expect(loginResponse).toBeTruthy();
    expect(loginResponse.status).toBe(200);
    
    // Verificar se o token está na resposta
    const loginBody = JSON.parse(loginResponse.body);
    expect(loginBody.success).toBe(true);
    expect(loginBody.token || token).toBeTruthy();
    
    // Aguardar atualização do estado
    await page.waitForTimeout(2000);
    
    // Verificar se saiu da tela de login
    const loginFormStillVisible = await loginForm.isVisible().catch(() => false);
    
    if (loginFormStillVisible) {
      await page.screenshot({ path: 'test-results/mobile-login-stuck.png', fullPage: true });
    }
    
    expect(loginFormStillVisible).toBeFalsy();
  });
});

