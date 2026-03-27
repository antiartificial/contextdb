document.addEventListener('DOMContentLoaded', function() {
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
});
