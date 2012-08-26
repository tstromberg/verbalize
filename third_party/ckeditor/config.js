/**
 * Copyright (c) 2003-2012, CKSource - Frederico Knabben. All rights reserved.
 * For licensing, see LICENSE.html or http://ckeditor.com/license
 */

CKEDITOR.editorConfig = function( config ) {
  // customized for verbalize
  config.toolbar = [[
      'Source', '-' ,

      'Format', 'Bold', 'Italic', 'Underline', 'StrikeThrough', '-',
      'NumberedList','BulletedList', '-',
      'Outdent', 'Indent','-',
      'Blockquote','CreateDiv',
    ],
    ['TextColor', 'BGColor' ],
    [ 'JustifyLeft','JustifyCenter','JustifyRight','JustifyBlock', '-',
      'BidiLtr','BidiRtl'],

    ['Link','Unlink','Anchor', 'Image', 'HorizontalRule',],
    ['Find', 'Replace'],
  ];
  config.height = 800;
};


