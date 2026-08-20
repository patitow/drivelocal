const { exec } = require('child_process');
const { promisify } = require('util');

const execAsync = promisify(exec);

async function testCompleteFix() {
  console.log('🚀 Iniciando teste completo de correção...\n');

  try {
    // 1. Test server debug
    console.log('1. Testando debug do servidor...');
    const { stdout: debugOutput } = await execAsync('node scripts/test-server-debug.js');
    console.log(debugOutput);
    console.log('✅ Debug do servidor concluído\n');

    // 2. Test path encoding
    console.log('2. Testando encoding de caminhos...');
    const { stdout: encodingOutput } = await execAsync('node scripts/test-path-encoding.js');
    console.log(encodingOutput);
    console.log('✅ Teste de encoding concluído\n');

    // 3. Test video functionality
    console.log('3. Testando funcionalidade de vídeo...');
    const { stdout: videoOutput } = await execAsync('node scripts/test-video-fix.js');
    console.log(videoOutput);
    console.log('✅ Teste de vídeo concluído\n');

    // 4. Test checkbox positioning
    console.log('4. Testando posicionamento de checkbox...');
    const { stdout: checkboxOutput } = await execAsync('node scripts/test-checkbox-position.js');
    console.log(checkboxOutput);
    console.log('✅ Teste de checkbox concluído\n');

    // 5. Test all fixes
    console.log('5. Testando todas as correções...');
    const { stdout: fixesOutput } = await execAsync('node scripts/test-fixes.js');
    console.log(fixesOutput);
    console.log('✅ Teste de correções concluído\n');

    console.log('🎉 Todos os testes foram executados com sucesso!');
    console.log('\n📋 Resumo dos testes:');
    console.log('  ✅ Debug do servidor');
    console.log('  ✅ Encoding de caminhos');
    console.log('  ✅ Funcionalidade de vídeo');
    console.log('  ✅ Posicionamento de checkbox');
    console.log('  ✅ Todas as correções');

  } catch (error) {
    console.error('❌ Erro durante os testes:', error.message);
    console.log('\n🔧 Possíveis soluções:');
    console.log('  1. Certifique-se de que o servidor está rodando: npm run dev');
    console.log('  2. Verifique se as portas 8080 e 3000 estão livres');
    console.log('  3. Execute: npm run kill-ports');
    console.log('  4. Execute: npm run dev');
  }
}

testCompleteFix().catch(console.error);
