import js from '@eslint/js'
import svelte from 'eslint-plugin-svelte'
import globals from 'globals'
import svelteParser from 'svelte-eslint-parser'
import tseslint from 'typescript-eslint'

/**
 * Le linter des deux écrans — celui du client et celui de l'administration.
 *
 * Il existe pour une raison précise, et elle n'est pas cosmétique : `svelte-check` ne
 * vérifie que des TYPES, et rien dans ce dépôt ne regardait jusqu'ici une promesse qu'on
 * oublie d'attendre, une condition qui ne peut pas être fausse, ou un `${}` posé sur un
 * objet qui rendra « [object Object] » à un bénévole.
 *
 * Le jeu est TYPÉ (`…TypeChecked`) : sans le type, la moitié de ce qui précède est
 * indécidable. Il coûte une passe de compilation à chaque exécution, ce qui est le prix
 * du seul contrôle capable de voir ces trois-là.
 *
 * DEUX RÉGIMES, et c'est délibéré :
 *
 *  - `src/` est du code LIVRÉ, tenu au jeu strict ;
 *  - `test/` est un banc, tenu au jeu recommandé. Un banc affirme volontiers ce que le
 *    strict interdit — `as HTMLElement` sur ce qu'il vient de poser dans le DOM, une
 *    comparaison que le type dit toujours vraie mais qui est justement ce qu'il vérifie.
 *    Les mesures sont dans le rapport du lot : 196 des 281 signalements du jeu strict
 *    tombaient là, et pas un ne décrivait un défaut.
 */
export default tseslint.config(
  {
    ignores: [
      'node_modules/**',
      'public/**',
      'testdata/**',
      // Le bundle COMMITÉ (§14.1). Il est produit par vite, pas écrit à la main.
      '../internal/web/dist/**',
    ],
  },

  js.configs.recommended,
  svelte.configs.recommended,

  {
    languageOptions: {
      parserOptions: {
        // Les fichiers d'outillage ne sont dans aucun `include` du tsconfig : ils sont
        // nommés ici plutôt que d'être laissés hors du linter.
        projectService: {
          allowDefaultProject: ['*.js', '*.ts', 'scripts/*.mjs'],
        },
        // Sans quoi le service de types ne sait pas quoi faire d'un `.svelte`, et
        // chaque composant sort une erreur d'analyse au lieu d'un verdict.
        extraFileExtensions: ['.svelte'],
        tsconfigRootDir: import.meta.dirname,
      },
      globals: { ...globals.browser },
    },
  },

  // Le code livré : le jeu strict.
  {
    files: ['src/**/*.ts', 'src/**/*.svelte'],
    extends: [tseslint.configs.strictTypeChecked, tseslint.configs.stylisticTypeChecked],
    rules: {
      /*
       * Les quatre règles que ce dépôt NE PEUT PAS tenir, avec ce qu'elles coûtaient.
       * Chacune est écartée pour une raison de conception, pas pour avoir la paix.
       */

      // 25 signalements. Le document de configuration voyage EXACTEMENT comme le fichier
      // l'écrit (§11.4) : `Draft.value` rend un `unknown`, et `String(unknown)` est ce que
      // `Draft.text` fait par contrat. La règle voit « [object Object] » là où le poste
      // a déjà refusé tout ce qui n'est pas une valeur scalaire.
      '@typescript-eslint/no-base-to-string': 'off',

      // 8 signalements, et ce sont des GARDE-FOUS VOULUS. `body.retired_keys ?? []` est
      // documenté sur place : « bien que le service ne serve plus null — ce poste peut
      // tourner un binaire plus ancien, et c'est exactement ce null qui rendait
      // l'administration inatteignable ». Le type décrit le binaire d'aujourd'hui ; le
      // navigateur, lui, parle à celui qui est installé.
      '@typescript-eslint/no-unnecessary-condition': 'off',

      // 6 signalements, purement stylistiques : `as HTMLElement` contre `!` sur les deux
      // points de montage. Aucun des deux ne vérifie quoi que ce soit à l'exécution.
      '@typescript-eslint/non-nullable-type-assertion-style': 'off',

      // 16 signalements, tous des NOMBRES. Ce qui doit rester interdit dans un `${}` est
      // l'objet — c'est lui qui rend « [object Object] » à un bénévole ; un entier rend
      // ses chiffres. La règle est donc gardée, et l'option dit lesquels sont sûrs.
      '@typescript-eslint/restrict-template-expressions': ['error', { allowNumber: true }],

      // 1 signalement, et c'est la convention que le dépôt suit déjà : `_` nomme ce
      // qu'une déstructuration doit sauter. La règle reste entière, on lui apprend le nom.
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrors: 'all' },
      ],

      /*
       * LA DETTE DE TYPAGE, nommée plutôt que tue — 11 signalements en tout.
       *
       * Elles décrivent toutes le même point : un `any` qui entre par une frontière non
       * typée — `JSON.parse` d'une réponse, une valeur d'un `Object.entries`, la propriété
       * `stack` d'un `catch`. Ce sont de vraies faiblesses, et aucune n'est un défaut
       * visible : le poste refuse déjà ce qui n'a pas la forme attendue, côté service.
       *
       * Les rendre rouges aujourd'hui demanderait de retyper huit fichiers que ce lot n'a
       * pas ouverts — dont l'écran client, qui n'est pas dans son périmètre. Elles sont
       * donc écartées ICI et nulle part ailleurs : le jour où ces frontières sont typées,
       * ce bloc disparaît d'un coup.
       */
      '@typescript-eslint/no-unsafe-argument': 'off', // 5, dont 3 sur un aperçu d'étiquette
      '@typescript-eslint/no-unsafe-return': 'off', // 2, App.svelte
      '@typescript-eslint/no-unsafe-assignment': 'off', // 2, App.svelte
      '@typescript-eslint/no-unsafe-member-access': 'off', // 1, error-net.ts
      '@typescript-eslint/restrict-plus-operands': 'off', // 3, même origine

      /*
       * LES SEPT DERNIÈRES, toutes stylistiques, une occurrence ou deux chacune.
       *
       * Aucune ne décrit un comportement : elles proposent une autre écriture de la même
       * chose. Les tenir coûterait de retoucher `App.svelte`, `Grid.svelte`, `Tile.svelte`
       * et `typography.ts` — quatre fichiers de l'écran CLIENT, celui qui tourne toute la
       * journée devant un client, pour un gain de forme et zéro gain de sens.
       */
      '@typescript-eslint/prefer-optional-chain': 'off', // 4
      '@typescript-eslint/no-confusing-void-expression': 'off', // 4 dans les `.ts`
      '@typescript-eslint/prefer-regexp-exec': 'off', // 1, Tile.svelte
      '@typescript-eslint/no-useless-default-assignment': 'off', // 1, Act.svelte
      '@typescript-eslint/no-dynamic-delete': 'off', // 1, et `Draft.unset` EST dynamique
      '@typescript-eslint/no-empty-function': 'off', // 1, un `onpick` de sonde qui ne fait rien
      '@typescript-eslint/no-misused-spread': 'off', // 1, typography.ts découpe de l'ASCII
      'no-useless-assignment': 'off', // 1, typography.ts
    },
  },

  // Les composants : ce que la règle de style TypeScript ne sait pas lire d'un gabarit.
  {
    files: ['src/**/*.svelte'],
    rules: {
      /*
       * 64 signalements, et pas un défaut parmi eux.
       *
       * `onclick={() => (dropping = false)}` est l'idiome d'un gestionnaire Svelte :
       * l'affectation vaut ce qu'elle affecte, et la flèche courte la renvoie. La règle
       * est écrite pour du TypeScript applicatif, où une fonction qui rend une valeur par
       * accident est un signal ; ici elle demanderait d'entourer d'accolades soixante-
       * quatre gestionnaires pour ne rien apprendre à personne.
       *
       * Elle reste ACTIVE sur les `.ts` : c'est là qu'elle attrape quelque chose.
       */
      '@typescript-eslint/no-confusing-void-expression': 'off',

      /*
       * 2 signalements, et la boucle sans clé est DÉLIBÉRÉE — le commentaire est dans
       * `FindingsPanel.svelte` : une ligne de CSV porte autant de signalements qu'elle a
       * de problèmes, donc `csv_line` n'est pas une clé, et un `each` clé dessus lèverait
       * `each_key_duplicate` sur le premier fichier venu, emportant tout l'écran.
       */
      'svelte/require-each-key': 'off',

      /*
       * 2 signalements. Les `Set` visés sont LOCAUX à un calcul — compter les rangées qui
       * portent un nom au plancher — et meurent avec lui. `SvelteSet` sert à un état
       * réactif partagé, ce qu'aucun des deux n'est.
       */
      'svelte/prefer-svelte-reactivity': 'off',
    },
  },

  /*
   * Les bancs : le jeu recommandé, et six règles de moins.
   *
   * Un banc affirme volontiers ce que le strict interdit, et il le fait exprès : une
   * assertion sur ce qu'il vient de poser dans le DOM, un `async` sans `await` pour
   * fabriquer une promesse, une promesse lâchée dont il vérifie justement qu'elle ne
   * casse rien. 46 des 64 signalements restants tombaient là, et pas un ne décrivait un
   * défaut. `web/test/` appartient d'ailleurs à quelqu'un d'autre : ce lot n'y touche pas.
   */
  {
    files: ['test/**/*.ts'],
    extends: [tseslint.configs.recommendedTypeChecked],
    languageOptions: { globals: { ...globals.node } },
    rules: {
      '@typescript-eslint/no-floating-promises': 'off',
      '@typescript-eslint/no-misused-promises': 'off',
      '@typescript-eslint/require-await': 'off',
      '@typescript-eslint/no-unnecessary-type-assertion': 'off',
      '@typescript-eslint/no-this-alias': 'off',
      '@typescript-eslint/no-base-to-string': 'off',
      'no-useless-assignment': 'off',
    },
  },

  // L'outillage : il tourne sous Node, et il n'est pas typé.
  {
    files: ['scripts/**/*.mjs', 'eslint.config.js', 'svelte.config.js', 'vite.config.ts'],
    extends: [tseslint.configs.disableTypeChecked],
    languageOptions: { globals: { ...globals.node } },
  },

  /*
   * LE CLIQUET DE TAILLE — la seule règle de ce fichier qui ne vient d'aucun preset.
   *
   * Elle répond à la question qui a motivé ce lot : qu'est-ce qui empêche une page de
   * regonfler ? Rien, jusqu'ici. Une page de deux mille lignes n'est pas mauvaise parce
   * qu'elle est longue, elle est mauvaise parce qu'on ne sait plus où regarder — et elle
   * y arrive une centaine de lignes à la fois, sans que personne ne décide rien.
   *
   * Les plafonds sont MESURÉS sur l'arbre du jour, pas choisis : le plus gros fichier de
   * chaque groupe, arrondi au-dessus avec environ un quart de marge. Ils ne demandent
   * donc rien à personne aujourd'hui, et ils refusent le doublement de demain.
   *
   * `skipComments` est délibéré : ce dépôt écrit ses raisons dans le code, et une règle
   * qui compterait la prose punirait exactement ce qu'il faut encourager.
   *
   * Chaque chiffre n'est écrit QU'UNE FOIS, à côté du plafond qu'il justifie : un
   * compteur recopié dans un second endroit finit toujours par mentir.
   */
  // Un composant d'administration : le plus gros en porte 209 (Maintenance.svelte).
  {
    files: ['src/admin/components/**/*.svelte'],
    rules: { 'max-lines': ['error', { max: 260, skipComments: true, skipBlankLines: true }] },
  },
  // Un module d'administration : le plus gros en porte 272 (lights.ts).
  {
    files: ['src/admin/lib/**/*.ts'],
    rules: { 'max-lines': ['error', { max: 320, skipComments: true, skipBlankLines: true }] },
  },
  // Un composant de l'écran client : le plus gros en porte 321 (Tile.svelte).
  {
    files: ['src/components/**/*.svelte'],
    rules: { 'max-lines': ['error', { max: 400, skipComments: true, skipBlankLines: true }] },
  },
  // Un module de l'écran client : le plus gros en porte 133 (dto.ts).
  {
    files: ['src/lib/**/*.ts'],
    rules: { 'max-lines': ['error', { max: 200, skipComments: true, skipBlankLines: true }] },
  },
  // Les deux coquilles, celle du client et celle de l'administration : 238 et 416.
  {
    files: ['src/App.svelte', 'src/admin/App.svelte'],
    rules: { 'max-lines': ['error', { max: 520, skipComments: true, skipBlankLines: true }] },
  },
  /*
   * Une page d'administration : `Catalog.svelte` en porte 1032, et c'est le plafond qui a
   * le moins de marge de tout ce fichier — six pour cent.
   *
   * POURQUOI CE CHIFFRE EST CE QU'IL EST. Ce qui reste dans cette page n'y est pas resté
   * par paresse : l'aperçu de grille et ses sondes, la zone de dépôt et les cinq actes
   * protégés sont retenus par des bancs qui lisent le TEXTE SOURCE du fichier — jusqu'à
   * `draftedFlag(\n                'ui.show_grid_prices',` à l'indentation près. Tant
   * qu'un banc épingle la forme du code plutôt que son comportement, la page ne peut pas
   * maigrir davantage.
   *
   * Le plafond descend donc le jour où ces bancs interrogeront le DOM plutôt que le
   * fichier, et il ne monte jamais.
   */
  {
    files: ['src/admin/pages/**/*.svelte'],
    rules: { 'max-lines': ['error', { max: 1100, skipComments: true, skipBlankLines: true }] },
  },

  /*
   * LE PARSER SVELTE VIENT EN DERNIER, et l'ordre n'est pas indifférent.
   *
   * Chaque `extends` de typescript-eslint repose son propre parser dans
   * `languageOptions` ; posé plus haut, celui de Svelte était donc écrasé pour les
   * fichiers auxquels le jeu strict s'applique, et chaque composant sortait un
   * « '>' expected » — le parser TypeScript essayant de lire du markup. Quarante
   * fichiers dans ce cas, tous les `.svelte` du dépôt.
   */
  {
    files: ['**/*.svelte', '**/*.svelte.ts'],
    languageOptions: {
      parser: svelteParser,
      parserOptions: {
        parser: tseslint.parser,
        projectService: true,
        extraFileExtensions: ['.svelte'],
        tsconfigRootDir: import.meta.dirname,
        svelteConfig: './svelte.config.js',
      },
    },
  },
)
