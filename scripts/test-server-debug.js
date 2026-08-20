const http = require('http');

async function testServerDebug() {
  console.log('🔍 Testando servidor e identificando problemas...\n');

  // Test 1: Health check
  console.log('1. Testando health check...');
  try {
    const healthResponse = await makeRequest('http://localhost:8080/api/health');
    console.log('✅ Health check:', healthResponse);
  } catch (error) {
    console.log('❌ Health check falhou:', error.message);
  }

  // Test 2: Files endpoint
  console.log('\n2. Testando endpoint de arquivos...');
  try {
    const filesResponse = await makeRequest('http://localhost:8080/api/files');
    console.log('✅ Files endpoint:', filesResponse);
    
    if (filesResponse.items && filesResponse.items.length > 0) {
      console.log(`📁 Encontrados ${filesResponse.items.length} itens`);
      
      // Test specific problematic files
      const videoFiles = filesResponse.items.filter(item => item.isVideo);
      const imageFiles = filesResponse.items.filter(item => item.isImage);
      
      console.log(`🎥 Arquivos de vídeo: ${videoFiles.length}`);
      console.log(`🖼️ Arquivos de imagem: ${imageFiles.length}`);
      
      if (videoFiles.length > 0) {
        const testVideo = videoFiles[0];
        console.log(`\n3. Testando vídeo: ${testVideo.name}`);
        console.log(`   Caminho: ${testVideo.path}`);
        
        // Test thumbnail
        console.log('   Testando thumbnail...');
        try {
          const thumbnailUrl = `http://localhost:8080/api/thumbnail/${encodeURIComponent(testVideo.path)}`;
          console.log(`   URL do thumbnail: ${thumbnailUrl}`);
          await makeRequest(thumbnailUrl, { method: 'HEAD' });
          console.log('   ✅ Thumbnail funcionando');
        } catch (error) {
          console.log('   ❌ Thumbnail falhou:', error.message);
        }
        
        // Test stream
        console.log('   Testando stream...');
        try {
          const streamUrl = `http://localhost:8080/api/stream/${encodeURIComponent(testVideo.path)}`;
          console.log(`   URL do stream: ${streamUrl}`);
          await makeRequest(streamUrl, { method: 'HEAD' });
          console.log('   ✅ Stream funcionando');
        } catch (error) {
          console.log('   ❌ Stream falhou:', error.message);
        }
        
        // Test download
        console.log('   Testando download...');
        try {
          const downloadUrl = `http://localhost:8080/api/download/${encodeURIComponent(testVideo.path)}`;
          console.log(`   URL do download: ${downloadUrl}`);
          await makeRequest(downloadUrl, { method: 'HEAD' });
          console.log('   ✅ Download funcionando');
        } catch (error) {
          console.log('   ❌ Download falhou:', error.message);
        }
      }
      
      if (imageFiles.length > 0) {
        const testImage = imageFiles[0];
        console.log(`\n4. Testando imagem: ${testImage.name}`);
        console.log(`   Caminho: ${testImage.path}`);
        
        // Test thumbnail
        console.log('   Testando thumbnail...');
        try {
          const thumbnailUrl = `http://localhost:8080/api/thumbnail/${encodeURIComponent(testImage.path)}`;
          console.log(`   URL do thumbnail: ${thumbnailUrl}`);
          await makeRequest(thumbnailUrl, { method: 'HEAD' });
          console.log('   ✅ Thumbnail funcionando');
        } catch (error) {
          console.log('   ❌ Thumbnail falhou:', error.message);
        }
      }
    }
  } catch (error) {
    console.log('❌ Files endpoint falhou:', error.message);
  }

  // Test 3: Check if server is running
  console.log('\n5. Verificando se o servidor está rodando...');
  try {
    await makeRequest('http://localhost:8080/api/health');
    console.log('✅ Servidor está rodando');
  } catch (error) {
    console.log('❌ Servidor não está rodando ou não está acessível');
    console.log('   Certifique-se de executar: npm run dev');
  }

  console.log('\n🏁 Teste de debug concluído!');
}

function makeRequest(url, options = {}) {
  return new Promise((resolve, reject) => {
    const requestOptions = {
      method: options.method || 'GET',
      headers: {
        'Accept': '*/*',
        'User-Agent': 'DriveLocal-Test'
      }
    };

    const req = http.request(url, requestOptions, (res) => {
      let data = '';
      
      res.on('data', (chunk) => {
        data += chunk;
      });
      
      res.on('end', () => {
        try {
          const jsonData = JSON.parse(data);
          resolve(jsonData);
        } catch (e) {
          resolve({
            status: res.statusCode,
            headers: res.headers,
            data: data.substring(0, 200) + (data.length > 200 ? '...' : '')
          });
        }
      });
    });

    req.on('error', (error) => {
      reject(error);
    });

    req.setTimeout(10000, () => {
      req.destroy();
      reject(new Error('Request timeout'));
    });

    req.end();
  });
}

testServerDebug().catch(console.error);
