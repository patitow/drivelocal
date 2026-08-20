const http = require('http');

async function testPathEncoding() {
  console.log('🔍 Testando encoding/decoding de caminhos...\n');

  // Test paths with backslashes (Windows style)
  const testPaths = [
    'OBS\\2025-06-30 19-33-13.mkv',
    'OUTROS\\NOVO\\Belle\\1000059591ce5cf8e61978db27.jpg',
    'cleanup safe\\f29126776.mp4',
    'VIDEOS\\test.mp4',
    'IMAGENS\\photo.jpg'
  ];

  console.log('Caminhos de teste:');
  testPaths.forEach((testPath, index) => {
    console.log(`  ${index + 1}. ${testPath}`);
  });

  console.log('\nTestando encoding/decoding:');
  testPaths.forEach((testPath, index) => {
    const encoded = encodeURIComponent(testPath);
    const decoded = decodeURIComponent(encoded);
    console.log(`  ${index + 1}. Original: ${testPath}`);
    console.log(`     Encoded: ${encoded}`);
    console.log(`     Decoded: ${decoded}`);
    console.log(`     Match: ${testPath === decoded ? '✅' : '❌'}`);
    console.log('');
  });

  // Test server endpoints
  console.log('Testando endpoints do servidor...\n');

  try {
    // First, get files list
    console.log('1. Obtendo lista de arquivos...');
    const filesResponse = await makeRequest('http://localhost:8080/api/files');
    
    if (filesResponse.items && filesResponse.items.length > 0) {
      console.log(`✅ Encontrados ${filesResponse.items.length} itens`);
      
      // Test with actual files from server
      const videoFiles = filesResponse.items.filter(item => item.isVideo);
      const imageFiles = filesResponse.items.filter(item => item.isImage);
      
      if (videoFiles.length > 0) {
        const testVideo = videoFiles[0];
        console.log(`\n2. Testando vídeo: ${testVideo.name}`);
        console.log(`   Caminho original: ${testVideo.path}`);
        
        const encodedPath = encodeURIComponent(testVideo.path);
        console.log(`   Caminho codificado: ${encodedPath}`);
        
        // Test thumbnail
        console.log('   Testando thumbnail...');
        try {
          const thumbnailUrl = `http://localhost:8080/api/thumbnail/${encodedPath}`;
          console.log(`   URL: ${thumbnailUrl}`);
          await makeRequest(thumbnailUrl, { method: 'HEAD' });
          console.log('   ✅ Thumbnail funcionando');
        } catch (error) {
          console.log('   ❌ Thumbnail falhou:', error.message);
        }
        
        // Test stream
        console.log('   Testando stream...');
        try {
          const streamUrl = `http://localhost:8080/api/stream/${encodedPath}`;
          console.log(`   URL: ${streamUrl}`);
          await makeRequest(streamUrl, { method: 'HEAD' });
          console.log('   ✅ Stream funcionando');
        } catch (error) {
          console.log('   ❌ Stream falhou:', error.message);
        }
      }
      
      if (imageFiles.length > 0) {
        const testImage = imageFiles[0];
        console.log(`\n3. Testando imagem: ${testImage.name}`);
        console.log(`   Caminho original: ${testImage.path}`);
        
        const encodedPath = encodeURIComponent(testImage.path);
        console.log(`   Caminho codificado: ${encodedPath}`);
        
        // Test thumbnail
        console.log('   Testando thumbnail...');
        try {
          const thumbnailUrl = `http://localhost:8080/api/thumbnail/${encodedPath}`;
          console.log(`   URL: ${thumbnailUrl}`);
          await makeRequest(thumbnailUrl, { method: 'HEAD' });
          console.log('   ✅ Thumbnail funcionando');
        } catch (error) {
          console.log('   ❌ Thumbnail falhou:', error.message);
        }
      }
    } else {
      console.log('❌ Nenhum arquivo encontrado');
    }
  } catch (error) {
    console.log('❌ Erro ao testar servidor:', error.message);
  }

  console.log('\n🏁 Teste de encoding concluído!');
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

testPathEncoding().catch(console.error);
