#/bin/sh

CREATE=${CREATE_SUPER_ADMIN_USER:false}
if [[ $CREATE = "true" ]]; then
  gotrue admin --instance_id=$INSTANCE_ID --aud=$AUD --superadmin=true createuser $USERNAME $PASSWORD
else
  echo "Skipped creation of user"
fi
