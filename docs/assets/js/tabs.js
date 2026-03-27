document.addEventListener('DOMContentLoaded', function() {
  // Code language tabs
  document.querySelectorAll('.code-tabs').forEach(function(tabs) {
    var buttons = tabs.querySelectorAll('.tab-btn');
    var contents = tabs.querySelectorAll('.tab-content');

    buttons.forEach(function(btn) {
      btn.addEventListener('click', function() {
        var target = btn.getAttribute('data-tab');
        buttons.forEach(function(b) { b.classList.remove('active'); });
        contents.forEach(function(c) { c.classList.remove('active'); });
        btn.classList.add('active');
        tabs.querySelector('[data-lang="' + target + '"]').classList.add('active');
      });
    });
  });

  // Dark/light mode toggle
  var toggle = document.getElementById('theme-toggle');
  if (toggle) {
    var saved = localStorage.getItem('contextdb-theme');
    if (saved) {
      document.documentElement.setAttribute('data-color-scheme', saved);
      updateToggleIcon(toggle, saved);
    }

    toggle.addEventListener('click', function() {
      var current = document.documentElement.getAttribute('data-color-scheme') || 'dark';
      var next = current === 'dark' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-color-scheme', next);
      localStorage.setItem('contextdb-theme', next);
      updateToggleIcon(toggle, next);
    });
  }

  function updateToggleIcon(el, scheme) {
    if (scheme === 'dark') {
      el.innerHTML = '<i class="fa-solid fa-sun"></i>';
      el.setAttribute('title', 'Switch to light mode');
    } else {
      el.innerHTML = '<i class="fa-solid fa-moon"></i>';
      el.setAttribute('title', 'Switch to dark mode');
    }
  }
});
